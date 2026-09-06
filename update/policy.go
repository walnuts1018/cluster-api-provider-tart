package update

import (
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
)

// ApplyModeはdomain/updateの同名型へのエイリアスである。
type ApplyMode = domainupdate.ApplyMode

const (
	ApplyModeNoReboot = domainupdate.ApplyModeNoReboot
	ApplyModeReboot   = domainupdate.ApplyModeReboot
)

// ErrPolicyUnknownはdomain/updateの同名エラーへのエイリアスである。
var ErrPolicyUnknown = domainupdate.ErrPolicyUnknown

// ResolveApplyModeはdomain/updateの純粋関数へ委譲する。
func ResolveApplyMode(strategy bootstrapv1alpha1.ConfigurationApplyStrategy) (ApplyMode, error) {
	return domainupdate.ResolveApplyMode(strategy)
}
