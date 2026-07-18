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

package agentapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	distributiondomain "github.com/walnuts1018/cluster-api-provider-tart/domain/node/entity/nodelifecycleengine"
	nodelifecycle "github.com/walnuts1018/cluster-api-provider-tart/domain/node/workflow/run_signed_step"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/agentsession"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
	k8sagentprogress "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/agentprogress"
	k8sagentsession "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/agentsession"
	k8sbootreport "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/bootreport"
	k8snodelifecycleengine "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/nodelifecycleengine"
)

const (
	testOperationUID       = "operation-uid"
	testCurrentVersion     = "v1.35.0"
	testTargetVersion      = "v1.36.0"
	testNodeLifecycleKeyID = "node-lifecycle-key"
)

var (
	testSignedPlan = agentprotocol.SignedPlan{Plan: agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  testOperationUID,
		HostUID:       "host-uid",
		OperationType: agentprotocol.OperationTypeProvision,
		Deadline:      time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-disk",
			SerialNumber: "disk-serial",
			MinSizeBytes: 1,
		},
		Artifact: &agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/os@sha256:" + strings.Repeat("b", 64),
			ManifestDigest: "sha256:" + strings.Repeat("c", 64),
			Generation:     1,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA},
		Steps:              []agentprotocol.PlanStep{{Name: "WriteImage"}},
	}}
	testPlanDigest = mustPlanDigest(testSignedPlan.Plan)
)

type staticResolver struct {
	key       client.ObjectKey
	operation *infrastructurev1beta1.TartHostOperation
}

func (resolver staticResolver) Resolve(
	_ context.Context,
	operationUID string,
) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error) {
	if operationUID != resolver.operation.Spec.OperationID {
		return client.ObjectKey{}, nil, errors.New("not found")
	}
	return resolver.key, resolver.operation.DeepCopy(), nil
}

type allowRegistration struct{}

func (allowRegistration) Verify(
	context.Context,
	*infrastructurev1beta1.TartHostOperation,
	string,
	agentprotocol.RegisterRequest,
) error {
	return nil
}

type staticBootstrap struct {
	bundle agentprotocol.BootstrapBundle
}

type staticPlan struct{}

func (staticPlan) GetPlan(context.Context, client.ObjectKey) (agentprotocol.SignedPlan, error) {
	return testSignedPlan, nil
}

type staticNodeLifecyclePlan struct {
	plan nodelifecycle.SignedPlan
	err  error
}

func (provider staticNodeLifecyclePlan) GetPlan(context.Context, client.ObjectKey) (nodelifecycle.SignedPlan, error) {
	return provider.plan, provider.err
}

func (provider staticBootstrap) GetBootstrapBundle(
	context.Context,
	client.ObjectKey,
) (agentprotocol.BootstrapBundle, error) {
	return provider.bundle, nil
}

type recordingBootReporter struct {
	request agentprotocol.BootReportRequest
	err     error
}

func (reporter *recordingBootReporter) ReportBoot(
	_ context.Context,
	_ client.ObjectKey,
	request agentprotocol.BootReportRequest,
	_ metav1.Time,
) error {
	reporter.request = request
	return reporter.err
}

type recordingNodeLifecycleStatus struct {
	operation   *infrastructurev1beta1.TartHostOperation
	plan        distributiondomain.Plan
	step        distributiondomain.Step
	snapshotRef *infrastructurev1beta1.ResourceReference
	failed      bool
}

func (status *recordingNodeLifecycleStatus) RecordStep(
	_ context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	plan distributiondomain.Plan,
	step distributiondomain.Step,
	snapshotRef *infrastructurev1beta1.ResourceReference,
) error {
	status.operation = operation
	status.plan = plan
	status.step = step
	status.snapshotRef = snapshotRef
	return nil
}

func (status *recordingNodeLifecycleStatus) MarkStepFailure(
	_ context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	status.operation = operation
	status.failed = true
	return nil
}

func TestHandlerRejectsPlainHTTPWithoutRedirect(t *testing.T) {
	handler := NewHandler(Config{})
	request := httptest.NewRequest(http.MethodGet, "/v1/operations/operation-uid/plan", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUpgradeRequired)
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want empty", location)
	}
}

func TestHandlerRejectsInvalidSessionOnEveryProtectedEndpoint(t *testing.T) {
	payload := []byte("#cloud-config\n")
	bootstrap := staticBootstrap{bundle: agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       payload,
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    "machine-uid",
		OperationUID:  testOperationUID,
	}}
	handler, _, _ := newAuthenticatedHandler(t, bootstrap)
	handler.config.BootReports = &recordingBootReporter{}
	requests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{
			name:   "plan",
			method: http.MethodGet,
			path:   "/v1/operations/operation-uid/plan",
		},
		{
			name:   "node lifecycle plan",
			method: http.MethodGet,
			path:   "/v1/operations/operation-uid/node-lifecycle-plan",
		},
		{
			name:   "progress",
			method: http.MethodPost,
			path:   "/v1/operations/operation-uid/progress",
			body: agentprotocol.ProgressRequest{
				APIVersion:    agentprotocol.APIVersion,
				OperationUID:  testOperationUID,
				PlanDigest:    testPlanDigest,
				AgentSequence: 1,
				Step:          "WriteImage",
				Percent:       100,
				Completed:     true,
			},
		},
		{
			name:   "node lifecycle progress",
			method: http.MethodPost,
			path:   "/v1/operations/operation-uid/node-lifecycle-progress",
			body: agentprotocol.NodeLifecycleProgressRequest{
				APIVersion:   agentprotocol.APIVersion,
				OperationUID: testOperationUID,
				PlanDigest:   testPlanDigest,
				Step:         string(distributiondomain.StepPreflightCompleted),
				Result:       agentprotocol.NodeLifecycleResultSucceeded,
			},
		},
		{
			name:   "bootstrap",
			method: http.MethodGet,
			path:   "/v1/operations/operation-uid/bootstrap",
		},
		{
			name:   "boot report",
			method: http.MethodPost,
			path:   "/v1/operations/operation-uid/boot-report",
			body: agentprotocol.BootReportRequest{
				APIVersion:         agentprotocol.APIVersion,
				OperationUID:       testOperationUID,
				PlanDigest:         testPlanDigest,
				BootID:             "boot-id",
				MachineID:          "machine-id",
				ActiveSlot:         "A",
				ArtifactGeneration: 1,
			},
		},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			response := performJSONRequest(
				t,
				handler,
				request.method,
				request.path,
				"token-for-another-host-or-operation",
				request.body,
			)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
		})
	}
}

func TestHandlerRejectsExpiredSession(t *testing.T) {
	handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
	handler.config.Now = func() time.Time {
		return time.Date(2026, 7, 5, 12, 11, 0, 0, time.UTC)
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/plan",
		sessionToken,
		nil,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestHandlerServesNodeLifecyclePlanAfterSessionAuthentication(t *testing.T) {
	handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
	signed := nodelifecycle.SignedPlan{
		Plan: nodelifecycle.Plan{
			APIVersion:       nodelifecycle.APIVersion,
			LifecycleRuntime: distributiondomain.LifecycleRuntimeKubeadm,
			OperationID:      testOperationUID,
			CurrentVersion:   testCurrentVersion,
			TargetVersion:    testTargetVersion,
			UpdateClass:      distributiondomain.UpdateClassKubernetesBinary,
			NodeRole:         distributiondomain.NodeRoleWorker,
			Deadline:         time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
			Steps:            []distributiondomain.Step{distributiondomain.StepPreflightCompleted},
		},
		Signature: agentprotocol.Signature{
			Algorithm: agentprotocol.SignatureAlgorithm,
			KeyID:     testNodeLifecycleKeyID,
			Value:     "signature",
		},
	}
	handler.config.NodeLifecyclePlans = staticNodeLifecyclePlan{plan: signed}
	validated, err := nodelifecycle.ValidatePlan(signed.Plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	planDigest, err := validated.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	handler.config.Operations = staticResolver{
		key: client.ObjectKey{Namespace: "default", Name: "operation"},
		operation: &infrastructurev1beta1.TartHostOperation{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "operation"},
			Spec: infrastructurev1beta1.TartHostOperationSpec{
				OperationID:             testOperationUID,
				NodeLifecyclePlanDigest: planDigest.String(),
				HostRef: infrastructurev1beta1.ResourceReference{
					Namespace: "default",
					Name:      "host",
					UID:       types.UID("host-uid"),
				},
			},
		},
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/node-lifecycle-plan",
		sessionToken,
		nil,
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	var got nodelifecycle.SignedPlan
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.Plan.OperationID != testOperationUID ||
		got.Plan.TargetVersion != testTargetVersion ||
		got.Signature.KeyID != testNodeLifecycleKeyID {
		t.Fatalf("node lifecycle plan = %#v, want signed plan", got)
	}
}

func TestHandlerRecordsNodeLifecycleStepAfterSessionAuthentication(t *testing.T) {
	handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
	recorder := &recordingNodeLifecycleStatus{}
	handler.config.NodeLifecycleStatus = recorder
	signed := handler.config.NodeLifecyclePlans.(staticNodeLifecyclePlan).plan
	validated, err := nodelifecycle.ValidatePlan(signed.Plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	planDigest, err := validated.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	operation := handler.config.Operations.(staticResolver).operation.DeepCopy()
	operation.Spec.NodeLifecyclePlanDigest = planDigest.String()
	handler.config.Operations = staticResolver{
		key:       client.ObjectKey{Namespace: operation.Namespace, Name: operation.Name},
		operation: operation,
	}
	body := agentprotocol.NodeLifecycleProgressRequest{
		APIVersion:   agentprotocol.APIVersion,
		OperationUID: testOperationUID,
		PlanDigest:   planDigest.String(),
		Step:         string(distributiondomain.StepPreflightCompleted),
		Result:       agentprotocol.NodeLifecycleResultSucceeded,
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/node-lifecycle-progress",
		sessionToken,
		body,
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if recorder.operation == nil || recorder.operation.Spec.OperationID != testOperationUID {
		t.Fatalf("recorded operation = %#v, want operation-uid", recorder.operation)
	}
	if recorder.plan.OperationID != testOperationUID ||
		recorder.step != distributiondomain.StepPreflightCompleted {
		t.Fatalf("recorded plan/step = %#v/%q, want PreflightCompleted", recorder.plan, recorder.step)
	}
}

func TestHandlerは各NodeLifecycleStep直後の再起動後も完了報告を重複記録しない(t *testing.T) {
	state := newAuthenticatedHandlerState(
		t,
		nil,
		nodelifecycle.Plan{
			APIVersion:       nodelifecycle.APIVersion,
			LifecycleRuntime: distributiondomain.LifecycleRuntimeKubeadm,
			OperationID:      testOperationUID,
			CurrentVersion:   testCurrentVersion,
			TargetVersion:    testTargetVersion,
			UpdateClass:      distributiondomain.UpdateClassKubernetesBinary,
			NodeRole:         distributiondomain.NodeRoleControlPlane,
			Deadline:         time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
			Steps: []distributiondomain.Step{
				distributiondomain.StepPreflightCompleted,
				distributiondomain.StepSnapshotCreated,
				distributiondomain.StepTargetSlotWritten,
				distributiondomain.StepDistributionApplied,
				distributiondomain.StepTargetSlotBooted,
				distributiondomain.StepHealthVerified,
				distributiondomain.StepCommitted,
			},
		},
		nil,
	)
	steps := []struct {
		step               distributiondomain.Step
		snapshotRef        string
		wantSnapshotRef    string
		wantLifecyclePhase string
		wantPhase          infrastructurev1beta1.TartHostOperationPhase
	}{
		{
			step:               distributiondomain.StepPreflightCompleted,
			wantLifecyclePhase: "Preflight",
			wantPhase:          infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		},
		{
			step:               distributiondomain.StepSnapshotCreated,
			snapshotRef:        "etcd-snapshot-1",
			wantSnapshotRef:    "etcd-snapshot-1",
			wantLifecyclePhase: "Snapshot",
			wantPhase:          infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		},
		{
			step:               distributiondomain.StepTargetSlotWritten,
			wantSnapshotRef:    "etcd-snapshot-1",
			wantLifecyclePhase: "Apply",
			wantPhase:          infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		},
		{
			step:               distributiondomain.StepDistributionApplied,
			wantSnapshotRef:    "etcd-snapshot-1",
			wantLifecyclePhase: "Apply",
			wantPhase:          infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		},
		{
			step:               distributiondomain.StepTargetSlotBooted,
			wantSnapshotRef:    "etcd-snapshot-1",
			wantLifecyclePhase: "Apply",
			wantPhase:          infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		},
		{
			step:               distributiondomain.StepHealthVerified,
			wantSnapshotRef:    "etcd-snapshot-1",
			wantLifecyclePhase: "Verify",
			wantPhase:          infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		},
		{
			step:               distributiondomain.StepCommitted,
			wantSnapshotRef:    "etcd-snapshot-1",
			wantLifecyclePhase: "Apply",
			wantPhase:          infrastructurev1beta1.TartHostOperationPhaseSucceeded,
		},
	}
	wantCompleted := make([]string, 0, len(steps))

	for _, test := range steps {
		body := agentprotocol.NodeLifecycleProgressRequest{
			APIVersion:   agentprotocol.APIVersion,
			OperationUID: testOperationUID,
			PlanDigest:   state.nodePlanDigest,
			Step:         string(test.step),
			Result:       agentprotocol.NodeLifecycleResultSucceeded,
			SnapshotRef:  test.snapshotRef,
		}
		response := performJSONRequest(
			t,
			state.newHandler(),
			http.MethodPost,
			"/v1/operations/operation-uid/node-lifecycle-progress",
			state.token,
			body,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
		}

		wantCompleted = append(wantCompleted, string(test.step))
		assertOperationStatus(
			t,
			state.k8sClient,
			state.key,
			wantCompleted,
			test.wantLifecyclePhase,
			test.wantPhase,
			test.wantSnapshotRef,
		)

		response = performJSONRequest(
			t,
			state.newHandler(),
			http.MethodPost,
			"/v1/operations/operation-uid/node-lifecycle-progress",
			state.token,
			body,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("duplicate status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
		}
		assertOperationStatus(
			t,
			state.k8sClient,
			state.key,
			wantCompleted,
			test.wantLifecyclePhase,
			test.wantPhase,
			test.wantSnapshotRef,
		)
	}
}

func TestHandlerは一時停止復帰後もfreshHandler経由で完了Stepを一度だけ保存する(t *testing.T) {
	state := newAuthenticatedHandlerState(
		t,
		nil,
		nodelifecycle.Plan{
			APIVersion:       nodelifecycle.APIVersion,
			LifecycleRuntime: distributiondomain.LifecycleRuntimeKubeadm,
			OperationID:      testOperationUID,
			CurrentVersion:   testCurrentVersion,
			TargetVersion:    testTargetVersion,
			UpdateClass:      distributiondomain.UpdateClassKubernetesBinary,
			NodeRole:         distributiondomain.NodeRoleControlPlane,
			Deadline:         time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
			Steps: []distributiondomain.Step{
				distributiondomain.StepPreflightCompleted,
				distributiondomain.StepHealthVerified,
			},
		},
		nil,
	)
	const outageCount = 3
	attempts := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts++
		if attempts <= outageCount {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusServiceUnavailable)
			if _, err := writer.Write([]byte(`{"code":"TemporaryUnavailable","message":"temporary outage"}`)); err != nil {
				t.Fatalf("writer.Write() error = %v", err)
			}
			return
		}
		state.newHandler().ServeHTTP(writer, request)
	})
	body := agentprotocol.NodeLifecycleProgressRequest{
		APIVersion:   agentprotocol.APIVersion,
		OperationUID: testOperationUID,
		PlanDigest:   state.nodePlanDigest,
		Step:         string(distributiondomain.StepPreflightCompleted),
		Result:       agentprotocol.NodeLifecycleResultSucceeded,
	}

	for range outageCount {
		response := performJSONRequest(
			t,
			handler,
			http.MethodPost,
			"/v1/operations/operation-uid/node-lifecycle-progress",
			state.token,
			body,
		)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
	}
	assertOperationStatus(
		t,
		state.k8sClient,
		state.key,
		nil,
		"",
		"",
		"",
	)

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/node-lifecycle-progress",
		state.token,
		body,
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	assertOperationStatus(
		t,
		state.k8sClient,
		state.key,
		[]string{"PreflightCompleted"},
		"Preflight",
		infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		"",
	)

	response = performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/node-lifecycle-progress",
		state.token,
		body,
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("duplicate status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	assertOperationStatus(
		t,
		state.k8sClient,
		state.key,
		[]string{"PreflightCompleted"},
		"Preflight",
		infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		"",
	)
}

func TestHandlerはStateMigration失敗時にSnapshotRefを保持したままRecoveryRequiredへ遷移する(t *testing.T) {
	state := newAuthenticatedHandlerState(
		t,
		nil,
		nodelifecycle.Plan{
			APIVersion:       nodelifecycle.APIVersion,
			LifecycleRuntime: distributiondomain.LifecycleRuntimeKubeadm,
			OperationID:      testOperationUID,
			CurrentVersion:   testCurrentVersion,
			TargetVersion:    testTargetVersion,
			UpdateClass:      distributiondomain.UpdateClassStateMigration,
			NodeRole:         distributiondomain.NodeRoleControlPlane,
			SnapshotRef:      "etcd-snapshot-1",
			Deadline:         time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
			Steps: []distributiondomain.Step{
				distributiondomain.StepPreflightCompleted,
				distributiondomain.StepSnapshotCreated,
				distributiondomain.StepDistributionApplied,
			},
		},
		func(operation *infrastructurev1beta1.TartHostOperation) {
			operation.Spec.UpdateClass = infrastructurev1beta1.UpdateClassStateMigration
		},
	)

	for _, body := range []agentprotocol.NodeLifecycleProgressRequest{
		{
			APIVersion:   agentprotocol.APIVersion,
			OperationUID: testOperationUID,
			PlanDigest:   state.nodePlanDigest,
			Step:         string(distributiondomain.StepPreflightCompleted),
			Result:       agentprotocol.NodeLifecycleResultSucceeded,
		},
		{
			APIVersion:   agentprotocol.APIVersion,
			OperationUID: testOperationUID,
			PlanDigest:   state.nodePlanDigest,
			Step:         string(distributiondomain.StepSnapshotCreated),
			Result:       agentprotocol.NodeLifecycleResultSucceeded,
			SnapshotRef:  "etcd-snapshot-1",
		},
	} {
		response := performJSONRequest(
			t,
			state.newHandler(),
			http.MethodPost,
			"/v1/operations/operation-uid/node-lifecycle-progress",
			state.token,
			body,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
		}
	}

	response := performJSONRequest(
		t,
		state.newHandler(),
		http.MethodPost,
		"/v1/operations/operation-uid/node-lifecycle-progress",
		state.token,
		agentprotocol.NodeLifecycleProgressRequest{
			APIVersion:   agentprotocol.APIVersion,
			OperationUID: testOperationUID,
			PlanDigest:   state.nodePlanDigest,
			Step:         string(distributiondomain.StepDistributionApplied),
			Result:       agentprotocol.NodeLifecycleResultFailed,
		},
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}

	current := getOperation(t, state.k8sClient, state.key)
	if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired {
		t.Fatalf("phase = %q, want RecoveryRequired", current.Status.Phase)
	}
	if current.Status.SnapshotRef == nil || current.Status.SnapshotRef.Name != "etcd-snapshot-1" {
		t.Fatalf("snapshotRef = %#v, want retained etcd-snapshot-1", current.Status.SnapshotRef)
	}
	if got := current.Status.CompletedSteps; len(got) != 2 || got[0] != "PreflightCompleted" || got[1] != "SnapshotCreated" {
		t.Fatalf("completedSteps = %#v, want PreflightCompleted/SnapshotCreated retained", got)
	}
}

func TestHandlerはKubernetesBinary失敗時にRollingBackへ遷移する(t *testing.T) {
	state := newAuthenticatedHandlerState(
		t,
		nil,
		nodelifecycle.Plan{
			APIVersion:       nodelifecycle.APIVersion,
			LifecycleRuntime: distributiondomain.LifecycleRuntimeKubeadm,
			OperationID:      testOperationUID,
			CurrentVersion:   testCurrentVersion,
			TargetVersion:    testTargetVersion,
			UpdateClass:      distributiondomain.UpdateClassKubernetesBinary,
			NodeRole:         distributiondomain.NodeRoleWorker,
			Deadline:         time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
			Steps: []distributiondomain.Step{
				distributiondomain.StepPreflightCompleted,
				distributiondomain.StepTargetSlotWritten,
				distributiondomain.StepDistributionApplied,
			},
		},
		func(operation *infrastructurev1beta1.TartHostOperation) {
			operation.Spec.UpdateClass = infrastructurev1beta1.UpdateClassKubernetesBinary
		},
	)

	for _, body := range []agentprotocol.NodeLifecycleProgressRequest{
		{
			APIVersion:   agentprotocol.APIVersion,
			OperationUID: testOperationUID,
			PlanDigest:   state.nodePlanDigest,
			Step:         string(distributiondomain.StepPreflightCompleted),
			Result:       agentprotocol.NodeLifecycleResultSucceeded,
		},
		{
			APIVersion:   agentprotocol.APIVersion,
			OperationUID: testOperationUID,
			PlanDigest:   state.nodePlanDigest,
			Step:         string(distributiondomain.StepTargetSlotWritten),
			Result:       agentprotocol.NodeLifecycleResultSucceeded,
		},
	} {
		response := performJSONRequest(
			t,
			state.newHandler(),
			http.MethodPost,
			"/v1/operations/operation-uid/node-lifecycle-progress",
			state.token,
			body,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
		}
	}

	response := performJSONRequest(
		t,
		state.newHandler(),
		http.MethodPost,
		"/v1/operations/operation-uid/node-lifecycle-progress",
		state.token,
		agentprotocol.NodeLifecycleProgressRequest{
			APIVersion:   agentprotocol.APIVersion,
			OperationUID: testOperationUID,
			PlanDigest:   state.nodePlanDigest,
			Step:         string(distributiondomain.StepDistributionApplied),
			Result:       agentprotocol.NodeLifecycleResultFailed,
		},
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}

	current := getOperation(t, state.k8sClient, state.key)
	if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack {
		t.Fatalf("phase = %q, want RollingBack", current.Status.Phase)
	}
	if got := current.Status.CompletedSteps; len(got) != 2 || got[0] != "PreflightCompleted" || got[1] != "TargetSlotWritten" {
		t.Fatalf("completedSteps = %#v, want PreflightCompleted/TargetSlotWritten retained", got)
	}
}

func TestHandlerErrorResponseDoesNotReflectCredentialOrRequestValue(t *testing.T) {
	handler, _, _ := newAuthenticatedHandler(t, nil)
	credential := "credential-that-must-not-be-reflected"
	secretValue := "request-value-that-must-not-be-reflected"
	body := agentprotocol.ProgressRequest{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  testOperationUID,
		PlanDigest:    testPlanDigest,
		AgentSequence: 1,
		Step:          secretValue,
		Percent:       100,
		Completed:     true,
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/progress",
		credential,
		body,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	for _, sensitive := range []string{credential, secretValue} {
		if strings.Contains(response.Body.String(), sensitive) {
			t.Fatalf("error response contains sensitive value %q", sensitive)
		}
	}
}

func TestHandlerProgressSequence(t *testing.T) {
	handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
	sequences := []int64{1, 2, 2, 1, 4, 3}
	wantStatuses := []int{
		http.StatusOK,
		http.StatusOK,
		http.StatusOK,
		http.StatusOK,
		http.StatusConflict,
		http.StatusOK,
	}
	for index, sequence := range sequences {
		body := agentprotocol.ProgressRequest{
			APIVersion:    agentprotocol.APIVersion,
			OperationUID:  testOperationUID,
			PlanDigest:    testPlanDigest,
			AgentSequence: sequence,
			Step:          "WriteImage",
			Percent:       100,
			Completed:     true,
		}
		response := performJSONRequest(
			t,
			handler,
			http.MethodPost,
			"/v1/operations/operation-uid/progress",
			sessionToken,
			body,
		)
		if response.Code != wantStatuses[index] {
			t.Fatalf("sequence %d status = %d, want %d; body=%s", sequence, response.Code, wantStatuses[index], response.Body.String())
		}
	}
}

func TestHandlerRejectsProgressStepOutsidePlan(t *testing.T) {
	handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
	body := agentprotocol.ProgressRequest{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  testOperationUID,
		PlanDigest:    testPlanDigest,
		AgentSequence: 1,
		Step:          "EraseState",
		Percent:       100,
		Completed:     true,
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/progress",
		sessionToken,
		body,
	)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestHandlerAcceptsValidBootReport(t *testing.T) {
	reporter := &recordingBootReporter{}
	handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
	handler.config.BootReports = reporter
	body := agentprotocol.BootReportRequest{
		APIVersion:             agentprotocol.APIVersion,
		OperationUID:           testOperationUID,
		PlanDigest:             testPlanDigest,
		BootID:                 "boot-id",
		MachineID:              "machine-id",
		ActiveSlot:             "A",
		ArtifactGeneration:     1,
		StateMounted:           true,
		DataMounted:            true,
		BootstrapApplied:       true,
		BootstrapPayloadDigest: "sha256:" + strings.Repeat("d", 64),
	}

	response := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/operations/operation-uid/boot-report",
		sessionToken,
		body,
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if reporter.request.BootID != body.BootID {
		t.Fatalf("reported boot ID = %q, want %q", reporter.request.BootID, body.BootID)
	}
}

func TestHandlerRejectsInvalidAndConflictingBootReports(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*agentprotocol.BootReportRequest)
		reportErr  error
		wantStatus int
	}{
		{
			name: "invalid active slot",
			mutate: func(report *agentprotocol.BootReportRequest) {
				report.ActiveSlot = "C"
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "operation phase conflict",
			mutate:     func(*agentprotocol.BootReportRequest) {},
			reportErr:  k8sbootreport.ErrReportConflict,
			wantStatus: http.StatusConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter := &recordingBootReporter{err: test.reportErr}
			handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
			handler.config.BootReports = reporter
			body := agentprotocol.BootReportRequest{
				APIVersion:         agentprotocol.APIVersion,
				OperationUID:       testOperationUID,
				PlanDigest:         testPlanDigest,
				BootID:             "boot-id",
				MachineID:          "machine-id",
				ActiveSlot:         "A",
				ArtifactGeneration: 1,
			}
			test.mutate(&body)

			response := performJSONRequest(
				t,
				handler,
				http.MethodPost,
				"/v1/operations/operation-uid/boot-report",
				sessionToken,
				body,
			)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestHandlerBootstrapIsSingleShot(t *testing.T) {
	payload := []byte("#cloud-config\npassword: highly-secret\n")
	bundle := agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       payload,
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    "machine-uid",
		OperationUID:  testOperationUID,
	}
	handler, sessionToken, _ := newAuthenticatedHandler(t, staticBootstrap{bundle: bundle})

	first := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/bootstrap",
		sessionToken,
		nil,
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	second := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/bootstrap",
		sessionToken,
		nil,
	)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second status = %d, want %d; body=%s", second.Code, http.StatusUnauthorized, second.Body.String())
	}
	if strings.Contains(second.Body.String(), "highly-secret") {
		t.Fatal("rejected response contains Bootstrap payload")
	}
}

func TestHandlerBootstrapStaysSingleShotAfterSessionReissue(t *testing.T) {
	payload := []byte("#cloud-config\npassword: highly-secret\n")
	bundle := agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       payload,
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    "machine-uid",
		OperationUID:  testOperationUID,
	}
	handler, sessionToken, k8sClient := newAuthenticatedHandler(t, staticBootstrap{bundle: bundle})

	first := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/bootstrap",
		sessionToken,
		nil,
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}

	sessions := k8sagentsession.NewService(k8sClient, agentsessiondomain.DefaultTTL)
	second, _, err := sessions.Issue(
		t.Context(),
		client.ObjectKey{Namespace: "default", Name: "operation"},
		"host-uid",
		testOperationUID,
		time.Date(2026, 7, 5, 12, 1, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("Issue(second) error = %v", err)
	}
	secondResponse := performJSONRequest(
		t,
		handler,
		http.MethodGet,
		"/v1/operations/operation-uid/bootstrap",
		second.BearerValue(),
		nil,
	)
	if secondResponse.Code != http.StatusUnauthorized {
		t.Fatalf("second session status = %d, want %d; body=%s", secondResponse.Code, http.StatusUnauthorized, secondResponse.Body.String())
	}
}

func TestHandlerRejectsRequestLargerThanOneMiB(t *testing.T) {
	handler, sessionToken, _ := newAuthenticatedHandler(t, nil)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/operations/operation-uid/progress",
		bytes.NewReader(make([]byte, agentprotocol.MaxRequestBodyBytes+1)),
	)
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Authorization", "Bearer "+sessionToken)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandlerRejectsUnsupportedFormatAndOversizedBootstrap(t *testing.T) {
	tests := []struct {
		name       string
		bundle     agentprotocol.BootstrapBundle
		wantStatus int
	}{
		{
			name: "unsupported format",
			bundle: agentprotocol.BootstrapBundle{
				APIVersion:    agentprotocol.APIVersion,
				Format:        "ignition",
				Payload:       []byte("payload"),
				PayloadDigest: digest.FromBytes([]byte("payload")).String(),
				MachineUID:    "machine-uid",
				OperationUID:  testOperationUID,
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "oversized payload",
			bundle: func() agentprotocol.BootstrapBundle {
				payload := make([]byte, agentprotocol.MaxBootstrapPayloadBytes+1)
				return agentprotocol.BootstrapBundle{
					APIVersion:    agentprotocol.APIVersion,
					Format:        agentprotocol.BootstrapFormatCloud,
					Payload:       payload,
					PayloadDigest: digest.FromBytes(payload).String(),
					MachineUID:    "machine-uid",
					OperationUID:  testOperationUID,
				}
			}(),
			wantStatus: http.StatusRequestEntityTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, token, _ := newAuthenticatedHandler(t, staticBootstrap{bundle: test.bundle})
			response := performJSONRequest(
				t,
				handler,
				http.MethodGet,
				"/v1/operations/operation-uid/bootstrap",
				token,
				nil,
			)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func newAuthenticatedHandler(t *testing.T, bootstrap BootstrapProvider) (*Handler, string, client.Client) {
	t.Helper()
	state := newAuthenticatedHandlerState(
		t,
		bootstrap,
		nodelifecycle.Plan{
			APIVersion:       nodelifecycle.APIVersion,
			LifecycleRuntime: distributiondomain.LifecycleRuntimeKubeadm,
			OperationID:      testOperationUID,
			CurrentVersion:   testCurrentVersion,
			TargetVersion:    testTargetVersion,
			UpdateClass:      distributiondomain.UpdateClassKubernetesBinary,
			NodeRole:         distributiondomain.NodeRoleWorker,
			Deadline:         time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
			Steps:            []distributiondomain.Step{distributiondomain.StepPreflightCompleted},
		},
		nil,
	)
	return state.newHandler(), state.token, state.k8sClient
}

type authenticatedHandlerState struct {
	token          string
	k8sClient      client.Client
	key            client.ObjectKey
	operation      *infrastructurev1beta1.TartHostOperation
	now            time.Time
	bootstrap      BootstrapProvider
	nodePlan       nodelifecycle.Plan
	nodePlanDigest string
}

func newAuthenticatedHandlerState(
	t *testing.T,
	bootstrap BootstrapProvider,
	nodePlan nodelifecycle.Plan,
	mutateOperation func(*infrastructurev1beta1.TartHostOperation),
) authenticatedHandlerState {
	t.Helper()
	key := client.ObjectKey{Namespace: "default", Name: "operation"}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace,
			Name:      key.Name,
			UID:       types.UID("operation-object-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: testOperationUID,
			PlanDigest:  testPlanDigest,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host",
				UID:       types.UID("host-uid"),
			},
		},
	}
	if mutateOperation != nil {
		mutateOperation(operation)
	}
	validatedNodePlan, err := nodelifecycle.ValidatePlan(nodePlan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	nodePlanDigest, err := validatedNodePlan.Digest()
	if err != nil {
		t.Fatalf("Node Lifecycle Plan.Digest() error = %v", err)
	}
	operation.Spec.NodeLifecyclePlanDigest = nodePlanDigest.String()
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	sessions := k8sagentsession.NewService(k8sClient, agentsessiondomain.DefaultTTL)
	token, _, err := sessions.Issue(t.Context(), key, "host-uid", testOperationUID, now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return authenticatedHandlerState{
		token:          token.BearerValue(),
		k8sClient:      k8sClient,
		key:            key,
		operation:      operation,
		now:            now,
		bootstrap:      bootstrap,
		nodePlan:       nodePlan,
		nodePlanDigest: nodePlanDigest.String(),
	}
}

func (state authenticatedHandlerState) newHandler() *Handler {
	sessions := k8sagentsession.NewService(state.k8sClient, agentsessiondomain.DefaultTTL)
	return NewHandler(Config{
		Operations:           staticResolver{key: state.key, operation: state.operation},
		RegistrationVerifier: allowRegistration{},
		Sessions:             sessions,
		Progress:             k8sagentprogress.NewService(state.k8sClient),
		Plans:                staticPlan{},
		NodeLifecyclePlans:   staticNodeLifecyclePlan{plan: nodelifecycle.SignedPlan{Plan: state.nodePlan}},
		NodeLifecycleStatus:  k8snodelifecycleengine.NewStatusStore(state.k8sClient),
		Bootstrap:            state.bootstrap,
		Now:                  func() time.Time { return state.now },
	})
}

func mustPlanDigest(plan agentprotocol.Plan) string {
	validated, err := agentprotocol.ValidatePlan(plan)
	if err != nil {
		panic(err)
	}
	digest, err := validated.Digest()
	if err != nil {
		panic(err)
	}
	return digest.String()
}

func performJSONRequest(
	t *testing.T,
	handler http.Handler,
	method, path, token string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func getOperation(t *testing.T, k8sClient client.Client, key client.ObjectKey) *infrastructurev1beta1.TartHostOperation {
	t.Helper()
	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), key, current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	return current
}

func assertOperationStatus(
	t *testing.T,
	k8sClient client.Client,
	key client.ObjectKey,
	wantCompleted []string,
	wantLifecyclePhase string,
	wantPhase infrastructurev1beta1.TartHostOperationPhase,
	wantSnapshotRef string,
) {
	t.Helper()
	current := getOperation(t, k8sClient, key)
	if len(current.Status.CompletedSteps) != len(wantCompleted) {
		t.Fatalf("completedSteps = %#v, want %#v", current.Status.CompletedSteps, wantCompleted)
	}
	for index, step := range wantCompleted {
		if current.Status.CompletedSteps[index] != step {
			t.Fatalf("completedSteps = %#v, want %#v", current.Status.CompletedSteps, wantCompleted)
		}
	}
	if current.Status.LifecyclePhase != wantLifecyclePhase {
		t.Fatalf("lifecyclePhase = %q, want %q", current.Status.LifecyclePhase, wantLifecyclePhase)
	}
	if current.Status.Phase != wantPhase {
		t.Fatalf("phase = %q, want %q", current.Status.Phase, wantPhase)
	}
	if wantSnapshotRef == "" {
		if current.Status.SnapshotRef != nil {
			t.Fatalf("snapshotRef = %#v, want nil", current.Status.SnapshotRef)
		}
		return
	}
	if current.Status.SnapshotRef == nil || current.Status.SnapshotRef.Name != wantSnapshotRef {
		t.Fatalf("snapshotRef = %#v, want %q", current.Status.SnapshotRef, wantSnapshotRef)
	}
}
