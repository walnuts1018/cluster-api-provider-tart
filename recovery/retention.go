package recovery

import (
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// CreationGracePeriodはrecovery Secretを作成してからTartHostのstatusへ参照が書かれるまでの間、GC判定を保留する猶予である。
// 参照countではなく現在のTartHost参照の観測でGCを判断するため、参照が書かれる前の一瞬をこの猶予で保護する。
const CreationGracePeriod = 30 * time.Minute

// IsReferencedByはTartHostが現在このrecovery Secretで復旧できるTalos installationを保持しているかを返す。
// recovery SecretはCA generationごとに分かれるため、cluster IDだけでなくSecret名の一致を要求する。
func IsReferencedBy(hostObject infrav1alpha1.TartHost, secretName string) bool {
	reference := hostObject.Status.CurrentTalosIdentityRef
	if reference == nil {
		return false
	}
	name := strings.TrimSpace(reference.RecoverySecretRef.Name)
	return name != "" && name == strings.TrimSpace(secretName)
}

// ShouldDeleteはrecovery Secretを削除してよいかを、現在のTartHost参照の観測から判定する。
// 1台でもこの旧Talos installationを保持しているTartHostが存在する間はfalseを返す。
// 参照countのような壊れやすい状態を持たず、reconcileのたびに現在のTartHost集合から再計算する。
func ShouldDelete(secret *corev1.Secret, hosts []infrav1alpha1.TartHost, now time.Time, grace time.Duration) bool {
	if secret == nil || !IsRecoverySecret(secret) || !secret.DeletionTimestamp.IsZero() {
		return false
	}
	if strings.TrimSpace(secret.Labels[ClusterIDLabel]) == "" {
		return false
	}
	if grace < 0 {
		grace = 0
	}
	if created := secret.CreationTimestamp.Time; !created.IsZero() && now.Sub(created) < grace {
		return false
	}
	for index := range hosts {
		if IsReferencedBy(hosts[index], secret.Name) {
			return false
		}
	}
	return true
}
