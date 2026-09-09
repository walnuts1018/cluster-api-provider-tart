package tartcontrolplane

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

// newCARotationTestClusterは、テストに必要な最小限のClusterID/ActiveSecretGenerationだけを
// 設定したTartClusterを構築する。
func newCARotationTestCluster(t *testing.T, clusterID string, activeGeneration int32, requestedGeneration *int32) *infrav1alpha1.TartCluster {
	t.Helper()
	parsed, err := clusterdomain.ParseClusterID(clusterID)
	if err != nil {
		t.Fatalf("ParseClusterID(%q) error = %v", clusterID, err)
	}
	cluster := &infrav1alpha1.TartCluster{}
	cluster.Name = "cluster-a"
	cluster.Spec.ClusterID = parsed
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
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := clusterv1.AddToScheme(scheme); err != nil {
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
	cluster := newCARotationTestCluster(t, "11111111-1111-1111-1111-111111111111", 1, nil)

	state, err := r.reconcileCARotation(context.Background(), cluster, false)
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
			cluster := newCARotationTestCluster(t, "11111111-1111-1111-1111-111111111111", 1, &requested)

			state, err := r.reconcileCARotation(context.Background(), cluster, false)
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
	cluster := newCARotationTestCluster(t, "11111111-1111-1111-1111-111111111111", -1, &requested)

	state, err := r.reconcileCARotation(context.Background(), cluster, false)
	if err == nil {
		t.Fatal("reconcileCARotation() error = nil, want an error for an invalid active secret generation")
	}
	if state.reason != "RotationGenerationInvalid" {
		t.Fatalf("reconcileCARotation() reason = %q, want %q", state.reason, "RotationGenerationInvalid")
	}
}

func TestReconcileCARotationStopsWhenMachineInventoryIsEmpty(t *testing.T) {
	t.Parallel()

	r := newCARotationTestReconciler(t)
	requested := int32(2)
	cluster := newCARotationTestCluster(t, "11111111-1111-1111-1111-111111111111", 1, &requested)
	cluster.Namespace = "default"

	state, err := r.reconcileCARotation(t.Context(), cluster, false)
	if err != nil {
		t.Fatalf("reconcileCARotation() error = %v", err)
	}
	if !state.active || state.reason != reasonMachineUnavailable {
		t.Fatalf("reconcileCARotation() state = %+v, want active MachineUnavailable", state)
	}
}

func TestPromoteCARotationRestoresStatusAndSecretLabel(t *testing.T) {
	t.Parallel()

	clusterID, err := clusterdomain.ParseClusterID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("ParseClusterID() error = %v", err)
	}
	cluster := newCARotationTestCluster(t, clusterID.String(), 1, new(int32))
	*cluster.Spec.CARotationRequestedGeneration = 2
	cluster.Namespace = "default"
	cluster.UID = "cluster-uid"
	controller := true
	secret, err := domaincontrolplane.BuildPendingSecret(
		cluster.Namespace,
		cluster.Name,
		clusterID,
		2,
		metav1.OwnerReference{
			APIVersion: infrav1alpha1.GroupVersion.String(),
			Kind:       "TartCluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
			Controller: &controller,
		},
		map[string][]byte{domaincontrolplane.BundleDataKey: []byte("bundle")},
	)
	if err != nil {
		t.Fatalf("BuildPendingSecret() error = %v", err)
	}
	oldSecret, err := domaincontrolplane.BuildActiveSecret(
		cluster.Namespace,
		cluster.Name,
		clusterID,
		1,
		metav1.OwnerReference{
			APIVersion: infrav1alpha1.GroupVersion.String(),
			Kind:       "TartCluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
			Controller: &controller,
		},
		map[string][]byte{domaincontrolplane.BundleDataKey: []byte("old-bundle")},
	)
	if err != nil {
		t.Fatalf("BuildActiveSecret() error = %v", err)
	}

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	r := &TartControlPlaneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, secret, oldSecret).WithStatusSubresource(cluster).Build()}
	if err := r.promoteCARotation(t.Context(), cluster, clusterID, 2); err != nil {
		t.Fatalf("promoteCARotation() error = %v", err)
	}

	var observedCluster infrav1alpha1.TartCluster
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(cluster), &observedCluster); err != nil {
		t.Fatalf("get promoted cluster: %v", err)
	}
	if observedCluster.Status.ActiveSecretGeneration != 2 {
		t.Fatalf("active secret generation = %d, want 2", observedCluster.Status.ActiveSecretGeneration)
	}
	var observedSecret corev1.Secret
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(secret), &observedSecret); err != nil {
		t.Fatalf("get promoted Secret: %v", err)
	}
	if observedSecret.Labels[domaincontrolplane.BundleStateLabel] != domaincontrolplane.BundleStateActive {
		t.Fatalf("bundle state = %q, want %q", observedSecret.Labels[domaincontrolplane.BundleStateLabel], domaincontrolplane.BundleStateActive)
	}
	var observedOldSecret corev1.Secret
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(oldSecret), &observedOldSecret); err != nil {
		t.Fatalf("get demoted Secret: %v", err)
	}
	if observedOldSecret.Labels[domaincontrolplane.BundleStateLabel] != domaincontrolplane.BundleStateRetired {
		t.Fatalf("old bundle state = %q, want %q", observedOldSecret.Labels[domaincontrolplane.BundleStateLabel], domaincontrolplane.BundleStateRetired)
	}
}

func TestPromoteCARotationRejectsRetiredTarget(t *testing.T) {
	t.Parallel()

	clusterID, err := clusterdomain.ParseClusterID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("ParseClusterID() error = %v", err)
	}
	cluster := newCARotationTestCluster(t, clusterID.String(), 1, new(int32))
	*cluster.Spec.CARotationRequestedGeneration = 2
	cluster.Namespace = "default"
	cluster.UID = "cluster-uid"
	controller := true
	secret, err := domaincontrolplane.BuildPendingSecret(
		cluster.Namespace,
		cluster.Name,
		clusterID,
		2,
		metav1.OwnerReference{
			APIVersion: infrav1alpha1.GroupVersion.String(),
			Kind:       "TartCluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
			Controller: &controller,
		},
		map[string][]byte{domaincontrolplane.BundleDataKey: []byte("bundle")},
	)
	if err != nil {
		t.Fatalf("BuildPendingSecret() error = %v", err)
	}
	secret.Labels[domaincontrolplane.BundleStateLabel] = domaincontrolplane.BundleStateRetired

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	r := &TartControlPlaneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, secret).WithStatusSubresource(cluster).Build()}
	if err := r.promoteCARotation(t.Context(), cluster, clusterID, 2); !errors.Is(err, errInvalidCARotationPromotionState) {
		t.Fatalf("promoteCARotation() error = %v, want errInvalidCARotationPromotionState", err)
	}

	var observedCluster infrav1alpha1.TartCluster
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(cluster), &observedCluster); err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if observedCluster.Status.ActiveSecretGeneration != 1 {
		t.Fatalf("active secret generation = %d, want 1 after rejected promotion", observedCluster.Status.ActiveSecretGeneration)
	}
	var observedSecret corev1.Secret
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(secret), &observedSecret); err != nil {
		t.Fatalf("get target Secret: %v", err)
	}
	if observedSecret.Labels[domaincontrolplane.BundleStateLabel] != domaincontrolplane.BundleStateRetired {
		t.Fatalf("target bundle state = %q, want %q after rejected promotion", observedSecret.Labels[domaincontrolplane.BundleStateLabel], domaincontrolplane.BundleStateRetired)
	}
}
