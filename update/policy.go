// Package updateは、稼働中Talos Nodeへmachine configuration差分をin-placeで適用してよいかどうかの判定を提供する。
// in-place updateとreboot-free updateは別概念である。rebootを伴う場合でも、同一CAPI Machine、同一TartMachine、
// 同一TartHost、同一local storageのまま「apply→controlled reboot→health recovery」で完結するならin-place updateであり、
// Machine replacementへfallbackすることはない。
package update

import (
	"errors"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
)

// ApplyModeはmachine configurationをTalosへどのmodeで適用するかを表す。
type ApplyMode string

const (
	// ApplyModeLiveはreboot-freeのapply(Talos ApplyConfigurationRequest_NO_REBOOT)を表す。
	ApplyModeLive ApplyMode = "Live"
	// ApplyModeRebootはapply後にcontrolled rebootを伴う適用を表す。
	ApplyModeReboot ApplyMode = "Reboot"
)

// ErrPolicyUnknownは解釈できないpolicyを表す。安全側に倒すため、判定できないpolicyは適用しない。
var ErrPolicyUnknown = errors.New("configuration update policy is unknown")

// autoResolvesToRebootは、Auto policyがreboot-freeのapplyを楽観的に試みてよいかどうかを表す唯一の判定点である。
// Talos 1.14のApplyConfigurationはAUTOモードをfield単位の安全性判定なしにNO_REBOOTへ読み替えるだけであり、
// 差分がreboot不要で反映されたことを保証しない。したがってAutoは常にrebootへ倒す。
// 将来Talosが信頼できるreboot要否判定APIを提供した場合は、この関数だけを変更してAutoを最適化する。
func autoResolvesToReboot() bool {
	return true
}

// ResolveApplyModeはconfiguration update policyから適用modeを決定する。
// InitialOnlyはupdateとして適用できないため、呼び出し側はEvaluateの分類でReprovisionRequiredとして扱う。
func ResolveApplyMode(policy bootstrapv1alpha1.ConfigurationUpdatePolicy) (ApplyMode, error) {
	switch policy {
	case bootstrapv1alpha1.ConfigurationUpdatePolicyAuto, "":
		if autoResolvesToReboot() {
			return ApplyModeReboot, nil
		}
		return ApplyModeLive, nil
	case bootstrapv1alpha1.ConfigurationUpdatePolicyLive:
		return ApplyModeLive, nil
	case bootstrapv1alpha1.ConfigurationUpdatePolicyReboot:
		return ApplyModeReboot, nil
	default:
		return "", ErrPolicyUnknown
	}
}
