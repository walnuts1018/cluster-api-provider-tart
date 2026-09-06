package tartcontrolplane

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// newCARotationTestClusterは、テストに必要な最小限のClusterID/ActiveSecretGenerationだけを
// 設定したTartClusterを構築する。
func newCARotationTestCluster(clusterID string, activeGeneration int32, requestedGeneration *int32) *infrav1alpha1.TartCluster {
	cluster := &infrav1alpha1.TartCluster{}
	cluster.Name = "cluster-a"
	cluster.Spec.ClusterID = clusterID
	cluster.Status.ActiveSecretGeneration = activeGeneration
	cluster.Spec.CARotationRequestedGeneration = requestedGeneration
	return cluster
}

func newCARotationTestReconciler(t *testing.T) *TartControlPlaneReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	return &TartControlPlaneReconciler{Client: fakeClient}
}

// TestReconcileCARotationNotRequestedは、caRotationRequestedGenerationが未設定の場合、
// activeにならず"NotRequested" reasonを返すことを検証する。
func TestReconcileCARotationNotRequested(t *testing.T) {
	t.Parallel()

	r := newCARotationTestReconciler(t)
	cluster := newCARotationTestCluster("11111111-1111-1111-1111-111111111111", 1, nil)

	state, err := r.reconcileCARotation(context.Background(), cluster, []clusterv1.Machine{}, false)
	if err != nil {
		t.Fatalf("reconcileCARotation() error = %v", err)
	}
	if state.active {
		t.Fatal("reconcileCARotation() active = true, want false when no rotation was requested")
	}
	if state.reason != "NotRequested" {
		t.Fatalf("reconcileCARotation() reason = %q, want %q", state.reason, "NotRequested")
	}
}

// TestReconcileCARotationInvalidRequestGenerationは、caRotationRequestedGenerationが
// 次世代(activeGeneration+1)と一致しない場合、"NotRequested"と混同せず区別できる専用のreasonで
// 停止することを検証する。ユーザーが要求を出したのに無視されたと誤解しないようにするための
// レビュー対応である。
func TestReconcileCARotationInvalidRequestGeneration(t *testing.T) {
	t.Parallel()

	r := newCARotationTestReconciler(t)
	tests := []struct {
		name      string
		requested int32
	}{
		{name: "stale generation behind active", requested: 1},
		{name: "skips ahead of the next generation", requested: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requested := tt.requested
			cluster := newCARotationTestCluster("11111111-1111-1111-1111-111111111111", 1, &requested)

			state, err := r.reconcileCARotation(context.Background(), cluster, []clusterv1.Machine{}, false)
			if err != nil {
				t.Fatalf("reconcileCARotation() error = %v", err)
			}
			if state.active {
				t.Fatal("reconcileCARotation() active = true, want false for an invalid rotation request (cluster keeps operating normally)")
			}
			if state.reason != "InvalidCARotationRequest" {
				t.Fatalf("reconcileCARotation() reason = %q, want %q", state.reason, "InvalidCARotationRequest")
			}
		})
	}
}

// TestReconcileCARotationInvalidActiveGenerationは、activeGeneration自体が不正で次世代を
// 計算できない場合、errorとしてfail-closedで停止することを検証する。
func TestReconcileCARotationInvalidActiveGeneration(t *testing.T) {
	t.Parallel()

	r := newCARotationTestReconciler(t)
	requested := int32(1)
	cluster := newCARotationTestCluster("11111111-1111-1111-1111-111111111111", -1, &requested)

	state, err := r.reconcileCARotation(context.Background(), cluster, []clusterv1.Machine{}, false)
	if err == nil {
		t.Fatal("reconcileCARotation() error = nil, want an error for an invalid active secret generation")
	}
	if state.reason != "RotationGenerationInvalid" {
		t.Fatalf("reconcileCARotation() reason = %q, want %q", state.reason, "RotationGenerationInvalid")
	}
}
