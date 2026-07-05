package agentapi

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
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestPlanWriterPersistsImmutableSignedPlan(t *testing.T) {
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

func TestPlanWriterRejectsOperationDigestMismatch(t *testing.T) {
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

func planWriterFixture(
	t *testing.T,
) (*infrastructurev1beta1.TartHostOperation, agentprotocol.ValidatedPlan, agentprotocol.Signature) {
	t.Helper()
	plan, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "0197d640-8d00-7a65-b67f-3f7c42a6935f",
		HostUID:       "host-a-uid",
		OperationType: agentprotocol.OperationTypeProvision,
		Deadline:      time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-test",
			SerialNumber: "disk-serial",
			MinSizeBytes: 64 * 1024 * 1024 * 1024,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/os@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ManifestDigest: digest.FromString("manifest").String(),
			Generation:     1,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA},
		Steps:              []agentprotocol.PlanStep{{Name: agentprotocol.StepWriteImage}},
	})
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature, err := agentprotocol.Sign(plan, "plan-key", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	planDigest, err := plan.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
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
			OperationID: plan.Value().OperationUID,
			PlanDigest:  planDigest.String(),
		},
	}
	return operation, plan, signature
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
