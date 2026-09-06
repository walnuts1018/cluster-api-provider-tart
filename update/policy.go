package update

import (
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
)

// ApplyModeはdomain/updateの同名型へのエイリアスである。
type ApplyMode = domainupdate.ApplyMode

const (
	ApplyModeLive   = domainupdate.ApplyModeLive
	ApplyModeReboot = domainupdate.ApplyModeReboot
)

// ErrPolicyUnknownはdomain/updateの同名エラーへのエイリアスである。
var ErrPolicyUnknown = domainupdate.ErrPolicyUnknown

// ResolveApplyModeはdomain/updateの純粋関数へ委譲する。
func ResolveApplyMode(policy bootstrapv1alpha1.ConfigurationUpdatePolicy) (ApplyMode, error) {
	return domainupdate.ResolveApplyMode(policy)
}
