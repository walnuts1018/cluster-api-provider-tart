package recovery

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// TestShouldDeleteは、recovery Secretの削除可否を参照countではなく現在のTartHost参照の観測から判断することを確認する。
func TestShouldDelete(t *testing.T) {
	t.Parallel()

	const clusterID = "6b1b8e56-0a2c-4a5b-9c1f-1f2b7f0a9c31"
	now := time.Unix(1_800_000_000, 0)
	created := metav1.NewTime(now.Add(-24 * time.Hour))
	secret := &corev1.Secret{
		Name:              SecretNamePrefix + clusterID,
		Namespace:         "tart-system",
		CreationTimestamp: created,
		Labels:            map[string]string{ClusterIDLabel: clusterID, SecretTypeLabel: SecretTypeRecovery},
	}
	holding := infrav1alpha1.TartHost{
		Status: infrav1alpha1.TartHostStatus{CurrentTalosIdentityRef: &infrav1alpha1.TalosIdentityReference{
			ClusterID:         clusterID,
			RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: secret.Name},
		}},
	}
	other := infrav1alpha1.TartHost{
		Status: infrav1alpha1.TartHostStatus{CurrentTalosIdentityRef: &infrav1alpha1.TalosIdentityReference{
			ClusterID:         "0a5e2f4a-2c67-4d13-8f0e-7a1cbb5f7d92",
			RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: "tart-talos-recovery-0a5e2f4a-2c67-4d13-8f0e-7a1cbb5f7d92"},
		}},
	}
	released := infrav1alpha1.TartHost{}

	tests := []struct {
		name   string
		secret *corev1.Secret
		hosts  []infrav1alpha1.TartHost
		want   bool
	}{
		{name: "a single holding Host keeps the recovery Secret", secret: secret, hosts: []infrav1alpha1.TartHost{released, other, holding}},
		{name: "no holding Host allows deletion", secret: secret, hosts: []infrav1alpha1.TartHost{released, other}, want: true},
		{name: "an empty inventory allows deletion", secret: secret, want: true},
		{
			name: "a recently created Secret is kept during the grace period",
			secret: func() *corev1.Secret {
				fresh := secret.DeepCopy()
				fresh.CreationTimestamp = metav1.NewTime(now.Add(-time.Minute))
				return fresh
			}(),
			want: false,
		},
		{
			name: "a Secret without the recovery label is never deleted by this controller",
			secret: func() *corev1.Secret {
				unlabeled := secret.DeepCopy()
				unlabeled.Labels = nil
				return unlabeled
			}(),
		},
		{
			name: "a Secret already being deleted is left alone",
			secret: func() *corev1.Secret {
				deleting := secret.DeepCopy()
				deleting.DeletionTimestamp = &created
				deleting.Finalizers = []string{"example.com/test"}
				return deleting
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldDelete(test.secret, test.hosts, now, CreationGracePeriod); got != test.want {
				t.Fatalf("ShouldDelete() = %v, want %v", got, test.want)
			}
		})
	}
}
