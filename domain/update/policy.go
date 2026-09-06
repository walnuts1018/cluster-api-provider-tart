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
	// ApplyModeNoRebootはTalosの通常applyをrebootなしで行うことを表す。
	ApplyModeNoReboot ApplyMode = "NoReboot"
	// ApplyModeRebootはapply後にcontrolled rebootを伴う適用を表す。
	ApplyModeReboot ApplyMode = "Reboot"
)

// ErrPolicyUnknownは解釈できないapply strategyを表す。
var ErrPolicyUnknown = errors.New("configuration apply strategy is unknown")

// ResolveApplyModeはconfiguration apply strategyから適用modeを決定する。
func ResolveApplyMode(strategy bootstrapv1alpha1.ConfigurationApplyStrategy) (ApplyMode, error) {
	switch strategy {
	case bootstrapv1alpha1.ConfigurationApplyStrategyReboot, "":
		return ApplyModeReboot, nil
	case bootstrapv1alpha1.ConfigurationApplyStrategyNoReboot:
		return ApplyModeNoReboot, nil
	default:
		return "", ErrPolicyUnknown
	}
}
