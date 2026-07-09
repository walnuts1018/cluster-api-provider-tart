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

package nodelifecycle

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestPlanWriterは署名済みNodeLifecyclePlanをImmutableSecretへ保存する(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	operation, plan, signature := planWriterFixture(t)
	k8sClient := newPlanWriterClient(t, operation)
	writer := NewPlanWriter(k8sClient)

	if err := writer.Write(ctx, operation, plan, signature); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Write(ctx, operation, plan, signature); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}

	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: operation.Namespace,
		Name:      operation.Name + PlanSecretSuffix,
	}, secret); err != nil {
		t.Fatalf("get Plan Secret: %v", err)
	}
	canonical, err := plan.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if string(secret.Data[PlanSecretPlanKey]) != string(canonical) {
		t.Fatalf("plan.json = %q, want canonical Plan", secret.Data[PlanSecretPlanKey])
	}
	if secret.Immutable == nil || !*secret.Immutable {
		t.Fatal("Plan Secret immutable = false, want true")
	}
	if len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].UID != operation.UID {
		t.Fatalf("ownerReferences = %#v", secret.OwnerReferences)
	}
}

func TestPlanWriterはOperationDigest不一致を拒否する(t *testing.T) {
	t.Parallel()

	operation, plan, signature := planWriterFixture(t)
	operation.Spec.PlanDigest = digest.FromString("different").String()
	err := NewPlanWriter(newPlanWriterClient(t, operation)).Write(
		t.Context(),
		operation,
		plan,
		signature,
	)
	if err == nil {
		t.Fatal("Write() accepted a mismatched Operation Plan digest")
	}
}

func TestProviderは保存済みNodeLifecyclePlanを読む(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	operation, plan, signature := planWriterFixture(t)
	k8sClient := newPlanWriterClient(t, operation)
	if err := NewPlanWriter(k8sClient).Write(ctx, operation, plan, signature); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	signed, err := NewProvider(k8sClient).GetPlan(ctx, client.ObjectKeyFromObject(operation))
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if signed.Plan.OperationID != plan.Value().OperationID ||
		signed.Plan.TargetVersion != plan.Value().TargetVersion ||
		signed.Signature != signature {
		t.Fatalf("GetPlan() = %#v, want saved signed plan", signed)
	}
}

func planWriterFixture(
	t *testing.T,
) (*infrastructurev1beta1.TartHostOperation, application.ValidatedPlan, agentprotocol.Signature) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	built, err := application.BuildSignedPlan(
		domain.Plan{
			OperationID:    "0197d640-8d00-7a65-b67f-3f7c42a6935f",
			CurrentVersion: "v1.35.0",
			TargetVersion:  "v1.36.0",
			UpdateClass:    domain.UpdateClassKubernetesBinary,
			NodeRole:       domain.NodeRoleWorker,
			Steps:          []domain.Step{domain.StepPreflightCompleted, domain.StepKubeadmApplied},
		},
		time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		"plan-key",
		privateKey,
	)
	if err != nil {
		t.Fatalf("BuildSignedPlan() error = %v", err)
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrastructurev1beta1.GroupVersion.String(),
			Kind:       "TartHostOperation",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-operation",
			Namespace: "default",
			UID:       types.UID("operation-object-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: built.Plan.Value().OperationID,
			PlanDigest:  built.Digest.String(),
		},
	}
	return operation, built.Plan, built.Signature
}

func newPlanWriterClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1.AddToScheme() error = %v", err)
	}
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("infrastructurev1beta1.AddToScheme() error = %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}
