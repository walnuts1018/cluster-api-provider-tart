package talosrecovery

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	recoveryusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/recovery"
)

func TestReconcileRecoverySecretRetentionAndDeletion(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core) error = %v", err)
	}
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(infrastructure) error = %v", err)
	}

	oldCreation := metav1.NewTime(time.Now().Add(-recoveryusecase.CreationGracePeriod - time.Hour))
	newRecoverySecret := func(name string) *corev1.Secret {
		return &corev1.Secret{
			Namespace:         "provider-system",
			Name:              name,
			UID:               types.UID(name + "-uid"),
			CreationTimestamp: oldCreation,
			Labels: map[string]string{
				recoveryusecase.SecretTypeLabel: recoveryusecase.SecretTypeRecovery,
				recoveryusecase.ClusterIDLabel:  "018f3c5e-5f8a-7c1b-9a2d-123456789abc",
			},
			Type: corev1.SecretTypeOpaque,
		}
	}

	tests := []struct {
		name            string
		request         types.NamespacedName
		secret          *corev1.Secret
		hosts           []*infrav1alpha1.TartHost
		wantExists      bool
		wantRequeue     bool
		managementSpace string
	}{
		{
			name:    "keeps referenced recovery secret",
			request: types.NamespacedName{Namespace: "provider-system", Name: "recovery-referenced"},
			secret:  newRecoverySecret("recovery-referenced"),
			hosts: []*infrav1alpha1.TartHost{{Status: infrav1alpha1.TartHostStatus{CurrentTalosIdentityRef: &infrav1alpha1.TalosIdentityReference{
				RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: "recovery-referenced"},
			}}}},
			wantExists:      true,
			wantRequeue:     true,
			managementSpace: "provider-system",
		},
		{
			name:            "deletes unreferenced old recovery secret",
			request:         types.NamespacedName{Namespace: "provider-system", Name: "recovery-unreferenced"},
			secret:          newRecoverySecret("recovery-unreferenced"),
			wantExists:      false,
			managementSpace: "provider-system",
		},
		{
			name:            "ignores non recovery secret",
			request:         types.NamespacedName{Namespace: "provider-system", Name: "ordinary"},
			secret:          &corev1.Secret{Namespace: "provider-system", Name: "ordinary"},
			wantExists:      true,
			managementSpace: "provider-system",
		},
		{
			name:    "ignores a secret outside management namespace",
			request: types.NamespacedName{Namespace: "other", Name: "recovery-outside"},
			secret: func() *corev1.Secret {
				secret := newRecoverySecret("recovery-outside")
				secret.Namespace = "other"
				return secret
			}(),
			wantExists:      true,
			managementSpace: "provider-system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			objects := []client.Object{tt.secret}
			for _, host := range tt.hosts {
				objects = append(objects, host)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			reconciler := &TalosRecoveryReconciler{Client: c, ManagementNamespace: tt.managementSpace}

			result, err := reconciler.Reconcile(t.Context(), reconcileRequest(tt.request))
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if result.RequeueAfter > 0 != tt.wantRequeue {
				t.Errorf("Reconcile() RequeueAfter = %s, want requeue=%t", result.RequeueAfter, tt.wantRequeue)
			}

			got := &corev1.Secret{}
			err = c.Get(t.Context(), client.ObjectKey{Namespace: tt.request.Namespace, Name: tt.request.Name}, got)
			if tt.wantExists && err != nil {
				t.Fatalf("Get() error = %v, want secret to remain", err)
			}
			if !tt.wantExists && !apierrors.IsNotFound(err) {
				t.Fatalf("Get() error = %v, want NotFound after recovery secret deletion", err)
			}
		})
	}
}

func reconcileRequest(key types.NamespacedName) ctrl.Request {
	return ctrl.Request{NamespacedName: key}
}
