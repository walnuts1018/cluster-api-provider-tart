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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

func TestProviderResolvesOperationAndReadsSignedPlan(t *testing.T) {
	ctx := t.Context()
	operation := testOperation()
	plan := testPlan()
	validated, err := agentprotocol.ValidatePlan(plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature, err := agentprotocol.Sign(validated, "test-key", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	planJSON, _ := json.Marshal(plan)
	signatureJSON, _ := json.Marshal(signature)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: operation.Namespace,
			Name:      operation.Name + PlanSecretSuffix,
		},
		Data: map[string][]byte{
			PlanSecretPlanKey:      planJSON,
			PlanSecretSignatureKey: signatureJSON,
		},
	}
	provider := NewProvider(newFakeClient(t, operation, secret))

	key, resolved, err := provider.Resolve(ctx, operation.Spec.OperationID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if key != client.ObjectKeyFromObject(operation) || resolved.Spec.OperationID != operation.Spec.OperationID {
		t.Fatalf("Resolve() = %#v, %#v", key, resolved)
	}
	signedPlan, err := provider.GetPlan(ctx, key)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if signedPlan.Signature.KeyID != "test-key" || signedPlan.Plan.OperationUID != operation.Spec.OperationID {
		t.Fatalf("GetPlan() = %#v", signedPlan)
	}
}

func TestProviderBuildsBootstrapBundleFromCABPKSecret(t *testing.T) {
	ctx := t.Context()
	operation := testOperation()
	machineUID := types.UID("capi-machine-uid")
	operation.Spec.MachineRef = &infrastructurev1beta1.ResourceReference{
		Namespace: "default",
		Name:      "tart-machine",
		UID:       types.UID("tart-machine-uid"),
	}
	tartMachine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "tart-machine",
			UID:       operation.Spec.MachineRef.UID,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "cluster.x-k8s.io/v1beta2",
				Kind:       "Machine",
				Name:       "capi-machine",
				UID:        machineUID,
			}},
		},
	}
	capiMachine := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2",
		"kind":       "Machine",
		"metadata": map[string]any{
			"namespace": "default",
			"name":      "capi-machine",
		},
		"spec": map[string]any{
			"bootstrap": map[string]any{"dataSecretName": "bootstrap-data"},
		},
	}}
	capiMachine.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Machine",
	})
	payload := []byte("#cloud-config\n")
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "bootstrap-data"},
		Data: map[string][]byte{
			BootstrapValueKey:  payload,
			BootstrapFormatKey: []byte(agentprotocol.BootstrapFormatCloud),
			BootstrapDigestKey: []byte(digest.FromBytes(payload).String()),
		},
	}
	provider := NewProvider(newFakeClient(t, operation, tartMachine, capiMachine, bootstrapSecret))

	bundle, err := provider.GetBootstrapBundle(ctx, client.ObjectKeyFromObject(operation))
	if err != nil {
		t.Fatalf("GetBootstrapBundle() error = %v", err)
	}
	if bundle.MachineUID != string(machineUID) ||
		bundle.OperationUID != operation.Spec.OperationID ||
		string(bundle.Payload) != string(payload) {
		t.Fatalf("GetBootstrapBundle() = %#v", bundle)
	}
}

func TestProviderRejectsBootstrapSecretWithoutCloudConfigFormat(t *testing.T) {
	ctx := t.Context()
	operation, tartMachine, capiMachine := testBootstrapObjects()
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "bootstrap-data"},
		Data: map[string][]byte{
			BootstrapValueKey: []byte("#cloud-config\n"),
		},
	}
	provider := NewProvider(newFakeClient(t, operation, tartMachine, capiMachine, bootstrapSecret))

	_, err := provider.GetBootstrapBundle(ctx, client.ObjectKeyFromObject(operation))
	if !errors.Is(err, agentprotocol.ErrUnsupportedBootstrapFormat) {
		t.Fatalf("GetBootstrapBundle() error = %v, want unsupported bootstrap format", err)
	}
}

func TestProviderRejectsBootstrapSecretWithMismatchedPayloadDigest(t *testing.T) {
	ctx := t.Context()
	operation, tartMachine, capiMachine := testBootstrapObjects()
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "bootstrap-data"},
		Data: map[string][]byte{
			BootstrapValueKey:  []byte("#cloud-config\n"),
			BootstrapFormatKey: []byte(agentprotocol.BootstrapFormatCloud),
			BootstrapDigestKey: []byte("sha256:" + strings.Repeat("d", 64)),
		},
	}
	provider := NewProvider(newFakeClient(t, operation, tartMachine, capiMachine, bootstrapSecret))

	_, err := provider.GetBootstrapBundle(ctx, client.ObjectKeyFromObject(operation))
	if err == nil || !strings.Contains(err.Error(), "payloadDigest") {
		t.Fatalf("GetBootstrapBundle() error = %v, want payloadDigest mismatch", err)
	}
}

func testBootstrapObjects() (
	*infrastructurev1beta1.TartHostOperation,
	*infrastructurev1beta1.TartMachine,
	*unstructured.Unstructured,
) {
	operation := testOperation()
	machineUID := types.UID("capi-machine-uid")
	operation.Spec.MachineRef = &infrastructurev1beta1.ResourceReference{
		Namespace: "default",
		Name:      "tart-machine",
		UID:       types.UID("tart-machine-uid"),
	}
	tartMachine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "tart-machine",
			UID:       operation.Spec.MachineRef.UID,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "cluster.x-k8s.io/v1beta2",
				Kind:       "Machine",
				Name:       "capi-machine",
				UID:        machineUID,
			}},
		},
	}
	capiMachine := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2",
		"kind":       "Machine",
		"metadata": map[string]any{
			"namespace": "default",
			"name":      "capi-machine",
		},
		"spec": map[string]any{
			"bootstrap": map[string]any{"dataSecretName": "bootstrap-data"},
		},
	}}
	capiMachine.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Machine",
	})
	return operation, tartMachine, capiMachine
}

func newFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1.AddToScheme() error = %v", err)
	}
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("infrastructurev1beta1.AddToScheme() error = %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&infrastructurev1beta1.TartHostOperation{}, OperationIDField, OperationIDIndex).
		WithObjects(objects...).
		Build()
}

func testOperation() *infrastructurev1beta1.TartHostOperation {
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "operation"},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			PlanDigest:  "sha256:" + strings.Repeat("a", 64),
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host",
				UID:       types.UID("host-uid"),
			},
		},
	}
}

func testPlan() agentprotocol.Plan {
	return agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-uid",
		HostUID:       "host-uid",
		OperationType: agentprotocol.OperationTypeProvision,
		Deadline:      time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-disk",
			SerialNumber: "disk",
			MinSizeBytes: 1,
		},
		Artifact: &agentprotocol.Artifact{
			Ref:            "oci://registry.test/os@sha256:" + strings.Repeat("b", 64),
			ManifestDigest: "sha256:" + strings.Repeat("c", 64),
			Generation:     1,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA},
		Steps:              []agentprotocol.PlanStep{{Name: "WriteImage"}},
	}
}
