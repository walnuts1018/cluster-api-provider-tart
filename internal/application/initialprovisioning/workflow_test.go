// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package initialprovisioning

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationhostallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/hostallocation"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
	domainhostallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestRequirementsForMachineUsesExactPlatformProfileContract(t *testing.T) {
	t.Parallel()

	machine := testMachine()
	requirements, err := requirementsForMachine(machine)
	if err != nil {
		t.Fatalf("requirementsForMachine() error = %v", err)
	}
	if requirements.Architecture != "amd64" || requirements.Firmware != "UEFI" {
		t.Fatalf("requirements = %#v", requirements)
	}
	const minimumDiskBytes = 64 * 1024 * 1024 * 1024
	if requirements.MinRootDiskBytes != minimumDiskBytes {
		t.Fatalf("MinRootDiskBytes = %d, want %d", requirements.MinRootDiskBytes, minimumDiskBytes)
	}
	requiredCapabilities, err := capability.NewSet(capability.PowerOn)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if !requirements.Capabilities.ContainsAll(requiredCapabilities) {
		t.Fatalf("Capabilities = %#v, want PowerOn", requirements.Capabilities)
	}

	machine.Spec.PlatformProfile = "amd64-uefi-unknown/v1"
	if _, err := requirementsForMachine(machine); err == nil {
		t.Fatal("requirementsForMachine() accepted an unknown profile")
	}
}

func TestDesiredObjectsDigestIsStableAndChangesWithMachineSpec(t *testing.T) {
	t.Parallel()

	machine := testMachine()
	first, err := desiredObjectsDigest(machine)
	if err != nil {
		t.Fatalf("desiredObjectsDigest() error = %v", err)
	}
	second, err := desiredObjectsDigest(machine.DeepCopy())
	if err != nil {
		t.Fatalf("desiredObjectsDigest(copy) error = %v", err)
	}
	if first != second || first == "sha256:0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("digest = %q and %q, want equal non-placeholder values", first, second)
	}

	changed := machine.DeepCopy()
	changed.Spec.Image.Ref = "oci://registry.test.walnuts.dev/os@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	changedDigest, err := desiredObjectsDigest(changed)
	if err != nil {
		t.Fatalf("desiredObjectsDigest(changed) error = %v", err)
	}
	if changedDigest == first {
		t.Fatal("digest did not change with TartMachine spec")
	}
}

func TestWorkflowReturnsAllocationPending(t *testing.T) {
	t.Parallel()

	workflow := NewWorkflow(
		hostReserveStub{},
		hostPhaseStub{},
		&operationServiceStub{},
		&recordingPlanWriter{},
		testPlanSigner(t),
	)
	result, err := workflow.Start(
		t.Context(),
		WorkflowInput{
			Machine:    testMachine(),
			MachineUID: "capi-machine-uid",
			Manifest:   validatedProvisionManifest(t),
		},
	)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pending, ok := result.(AllocationPending)
	if !ok {
		t.Fatalf("Start() result = %T, want AllocationPending", result)
	}
	if pending.Reason != "NoAvailableHost" {
		t.Fatalf("Reason = %q, want NoAvailableHost", pending.Reason)
	}
}

func TestWorkflowPersistsProvisionPlanAfterOperationStart(t *testing.T) {
	t.Parallel()

	signer := testPlanSigner(t)
	writer := &recordingPlanWriter{}
	host := matchingProvisionHost()
	host.Labels = map[string]string{"rack": "a"}
	workflow := NewWorkflow(
		hostReserveStub{host: host},
		hostPhaseStub{},
		&operationServiceStub{},
		writer,
		signer,
	)

	result, err := workflow.Start(t.Context(), WorkflowInput{
		Machine:    testMachine(),
		MachineUID: "capi-machine-uid",
		Manifest:   validatedProvisionManifest(t),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	started, ok := result.(Started)
	if !ok {
		t.Fatalf("Start() result = %T, want Started", result)
	}
	if writer.calls != 1 {
		t.Fatalf("PlanWriter calls = %d, want 1", writer.calls)
	}
	if writer.operation == nil || writer.operation.Spec.PlanDigest != started.Operation.Spec.PlanDigest {
		t.Fatalf("operation = %#v, want plan digest to match started operation", writer.operation)
	}
	digest, err := writer.plan.Digest()
	if err != nil {
		t.Fatalf("plan.Digest() error = %v", err)
	}
	if digest.String() != started.Operation.Spec.PlanDigest {
		t.Fatalf("persisted plan digest = %q, want %q", digest, started.Operation.Spec.PlanDigest)
	}
	if err := agentprotocol.VerifySignature(
		writer.plan,
		writer.signature,
		agentprotocol.StaticTrustStore{signer.KeyID: signer.PrivateKey.Public().(ed25519.PublicKey)},
	); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
}

func TestWorkflowReturnsPlanWriterError(t *testing.T) {
	t.Parallel()

	signer := testPlanSigner(t)
	writer := &recordingPlanWriter{err: errors.New("plan writer failed")}
	host := matchingProvisionHost()
	host.Labels = map[string]string{"rack": "a"}
	workflow := NewWorkflow(
		hostReserveStub{host: host},
		hostPhaseStub{},
		&operationServiceStub{},
		writer,
		signer,
	)

	_, err := workflow.Start(t.Context(), WorkflowInput{
		Machine:    testMachine(),
		MachineUID: "capi-machine-uid",
		Manifest:   validatedProvisionManifest(t),
	})
	if err == nil {
		t.Fatal("Start() succeeded unexpectedly")
	}
}

func TestWorkflowCompletesProvisioningInOperationThenHostOrder(t *testing.T) {
	t.Parallel()

	var calls []string
	workflow := NewWorkflow(
		hostReserveStub{},
		hostPhaseStub{markProvisioned: func() { calls = append(calls, "host") }},
		&operationServiceStub{completeProvision: func() { calls = append(calls, "operation") }},
		&recordingPlanWriter{},
		testPlanSigner(t),
	)

	if err := workflow.CompleteProvisioning(
		t.Context(),
		&infrastructurev1beta1.TartHost{},
		&infrastructurev1beta1.TartHostOperation{},
	); err != nil {
		t.Fatalf("CompleteProvisioning() error = %v", err)
	}
	if len(calls) != 2 || calls[0] != "operation" || calls[1] != "host" {
		t.Fatalf("call order = %v, want [operation host]", calls)
	}
}

func TestWorkflowStopsProvisionCompletionWhenOperationCompletionFails(t *testing.T) {
	t.Parallel()

	hostMarked := false
	workflow := NewWorkflow(
		hostReserveStub{},
		hostPhaseStub{
			markProvisioned: func() { hostMarked = true },
		},
		&operationServiceStub{
			err: errors.New("operation failed"),
		},
		&recordingPlanWriter{},
		testPlanSigner(t),
	)

	err := workflow.CompleteProvisioning(
		t.Context(),
		&infrastructurev1beta1.TartHost{},
		&infrastructurev1beta1.TartHostOperation{},
	)
	if err == nil {
		t.Fatal("CompleteProvisioning() succeeded unexpectedly")
	}
	if hostMarked {
		t.Fatal("MarkHostProvisioned() was called after operation completion failed")
	}
}

func testMachine() *infrastructurev1beta1.TartMachine {
	return &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image: infrastructurev1beta1.ImageSpec{
				Ref: "oci://registry.test.walnuts.dev/os@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			PlatformProfile: "amd64-uefi-ab/v1",
			HostSelector: infrastructurev1beta1.HostSelector{
				MatchLabels: map[string]string{"rack": "a"},
			},
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
		},
	}
}

type hostReserveStub struct {
	host *infrastructurev1beta1.TartHost
}

func (s hostReserveStub) ListCandidates(
	context.Context,
	*infrastructurev1beta1.TartMachine,
) ([]domainhostallocation.Candidate, error) {
	if s.host == nil {
		return nil, nil
	}
	requiredCapabilities, err := capability.NewSet(capability.PowerOn)
	if err != nil {
		return nil, err
	}
	return []domainhostallocation.Candidate{
		{
			Host: domainhostallocation.HostRef{
				Namespace: s.host.Namespace,
				Name:      s.host.Name,
				UID:       string(s.host.UID),
			},
			Phase:             hostdomain.PhaseAvailable,
			Assignment:        domainhostallocation.Unassigned{},
			Architecture:      string(s.host.Spec.Architecture),
			Firmware:          string(s.host.Spec.Firmware),
			PlatformProfile:   s.host.Spec.PlatformProfile,
			RootDiskSizeBytes: s.host.Spec.RootDeviceHints.MinSizeBytes,
			Capabilities:      requiredCapabilities,
			Labels:            s.host.Labels,
		},
	}, nil
}

func (s hostReserveStub) ReserveCandidate(
	context.Context,
	*infrastructurev1beta1.TartMachine,
	domainhostallocation.HostRef,
) (applicationhostallocation.ReservationResult, error) {
	if s.host == nil {
		return applicationhostallocation.RetrySelection{}, nil
	}
	return applicationhostallocation.Reserved{Host: s.host}, nil
}

type hostPhaseStub struct {
	err             error
	markProvisioned func()
}

func (s hostPhaseStub) ReserveForMachine(
	context.Context,
	*infrastructurev1beta1.TartHost,
	*infrastructurev1beta1.TartMachine,
) error {
	return s.err
}

func (s hostPhaseStub) MarkHostProvisioned(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	if s.markProvisioned != nil {
		s.markProvisioned()
	}
	return s.err
}

type operationServiceStub struct {
	operation         *infrastructurev1beta1.TartHostOperation
	err               error
	completeProvision func()
	startCalls        int
}

func (s *operationServiceStub) Start(
	_ context.Context,
	desired *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	s.startCalls++
	if s.err != nil {
		return nil, s.err
	}
	if s.operation != nil {
		return s.operation, nil
	}
	started := desired.DeepCopy()
	name, err := operationdomain.ResourceName(string(desired.Spec.HostRef.UID))
	if err != nil {
		return nil, err
	}
	started.Name = name
	if started.UID == "" {
		started.UID = types.UID("operation-uid")
	}
	return started, nil
}

func (s operationServiceStub) CompleteProvision(
	context.Context,
	*infrastructurev1beta1.TartHostOperation,
) error {
	if s.completeProvision != nil {
		s.completeProvision()
	}
	return s.err
}

type recordingPlanWriter struct {
	calls     int
	operation *infrastructurev1beta1.TartHostOperation
	plan      agentprotocol.ValidatedPlan
	signature agentprotocol.Signature
	err       error
}

func (writer *recordingPlanWriter) Write(
	_ context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	plan agentprotocol.ValidatedPlan,
	signature agentprotocol.Signature,
) error {
	writer.calls++
	writer.operation = operation.DeepCopy()
	writer.plan = plan
	writer.signature = signature
	return writer.err
}

func testPlanSigner(t *testing.T) PlanSigner {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	_ = publicKey
	return PlanSigner{
		KeyID:      "plan-signer",
		PrivateKey: privateKey,
	}
}
