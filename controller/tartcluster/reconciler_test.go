package tartcluster

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/certbuilder"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

func TestReconstructActiveSecretGeneration(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core) error = %v", err)
	}
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(infrastructure) error = %v", err)
	}
	clusterID, err := clusterdomain.ParseClusterID("018f3c5e-5f8a-7c1b-9a2d-123456789abc")
	if err != nil {
		t.Fatalf("ParseClusterID() error = %v", err)
	}
	cluster := new(infrav1alpha1.TartCluster)
	cluster.ObjectMeta = metav1.ObjectMeta{Namespace: "cluster-a", Name: "cluster-a", UID: types.UID("cluster-a-uid")}
	cluster.Spec = infrav1alpha1.TartClusterSpec{ClusterID: clusterID.String()}
	buildSecret := func(generation int32) *corev1.Secret {
		t.Helper()
		data, err := certbuilder.GenerateBundleData(clusterID)
		if err != nil {
			t.Fatalf("GenerateBundleData() error = %v", err)
		}
		secret, err := domaincontrolplane.BuildActiveSecret(cluster.Namespace, cluster.Name, clusterID, generation, metav1.OwnerReference{
			APIVersion: infrav1alpha1.GroupVersion.String(),
			Kind:       controller.TartClusterKind,
			Name:       cluster.Name,
			UID:        cluster.UID,
		}, data)
		if err != nil {
			t.Fatalf("BuildActiveSecret() error = %v", err)
		}
		return secret
	}

	t.Run("defaults to generation one when no bundle exists", func(t *testing.T) {
		t.Parallel()
		reconciler := &TartClusterReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
		generation, err := reconciler.reconstructActiveSecretGeneration(t.Context(), cluster, clusterID)
		if err != nil {
			t.Fatalf("reconstructActiveSecretGeneration() error = %v", err)
		}
		if generation != 1 {
			t.Fatalf("reconstructActiveSecretGeneration() = %d, want 1", generation)
		}
	})

	t.Run("restores the unique active generation", func(t *testing.T) {
		t.Parallel()
		reconciler := &TartClusterReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(buildSecret(4)).Build()}
		generation, err := reconciler.reconstructActiveSecretGeneration(t.Context(), cluster, clusterID)
		if err != nil {
			t.Fatalf("reconstructActiveSecretGeneration() error = %v", err)
		}
		if generation != 4 {
			t.Fatalf("reconstructActiveSecretGeneration() = %d, want 4", generation)
		}
	})

	t.Run("refuses ambiguous active generations", func(t *testing.T) {
		t.Parallel()
		reconciler := &TartClusterReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(buildSecret(2), buildSecret(3)).Build()}
		if _, err := reconciler.reconstructActiveSecretGeneration(t.Context(), cluster, clusterID); err == nil {
			t.Fatal("reconstructActiveSecretGeneration() error = nil, want ambiguous Active Secret failure")
		}
	})
}
