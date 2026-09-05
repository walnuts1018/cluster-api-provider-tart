// Package hostはHost選択とallocation eligibilityの純粋なpolicy、およびTartHost.spec.consumerRefのatomic compare-and-swap claim adapterを提供する。詳細は.agents/skills/host-lifecycle/SKILL.mdを参照する。
package host

import (
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// EligibilityはHostをallocation対象にできるかという観測結果である。workflow phaseとして保存せず、spec.consumerRef、spec.retainedFrom、spec.reusePolicy、spec.reuseApproval、spec.reuseModeから常に再計算する。
type Eligibility string

const (
	// Available HostにはconsumerRefとretainedFromがないため、決定論的なHost allocatorがclaimできる。
	Available Eligibility = "Available"
	// Claimed HostにはconsumerRefがあり、特定のTartMachineへbindされている。
	Claimed Eligibility = "Claimed"
	// Retained Hostは以前のMachineのdataまたはTalos identityを保持するため、現在のretainedFromに一致する明示的なreuse approvalがあるまで自動allocationしない。
	Retained Eligibility = "Retained"
	// Reusable Hostは一致するreuse approvalとreuse modeを持つRetained Hostである。AdoptまたはReprovisionの対象にはできるが、reuseModeを無視した通常のclaim経路では選択しない。
	Reusable Eligibility = "Reusable"
)

// ClassifyはHostのSpecだけから現在のallocation eligibilityを計算する。外部副作用を実行せず、HostがfreshまたはClaimedの間に設定されたreusePolicyやreuseApprovalを将来の削除承認として扱わない。これらのfieldはHostがRetainedとなりpreviousConsumerRefが存在した場合だけ有効になる。
func Classify(spec infrav1alpha1.TartHostSpec) Eligibility {
	if spec.ConsumerRef != nil {
		return Claimed
	}
	if spec.PreviousConsumerRef == nil {
		return Available
	}
	if spec.ReusePolicy == infrav1alpha1.ReusePolicyAllowReuse &&
		spec.ReuseApproval != nil &&
		spec.PreviousConsumerRef.UID != "" &&
		spec.ReuseApproval.PreviousConsumerUID != "" &&
		spec.ReuseApproval.PreviousConsumerUID == spec.PreviousConsumerRef.UID &&
		validReuseMode(spec.ReuseMode) {
		return Reusable
	}
	return Retained
}

// ReprovisionApprovedは、Retained Hostに対する破壊的なTalos Resetがユーザーによって明示的に承認されているかを返す。
// Classifyと異なりconsumerRefの有無に依存しないため、claim確立後のreconcileでも同じ承認を再評価できる。
// reuse approvalは常に現在のpreviousConsumerRef.UIDと一致する必要があり、次のMachine削除でpreviousConsumerRefが変われば自動的に無効になる。
func ReprovisionApproved(spec infrav1alpha1.TartHostSpec) bool {
	return spec.ReusePolicy == infrav1alpha1.ReusePolicyAllowReuse &&
		spec.ReuseMode == infrav1alpha1.ReuseModeReprovision &&
		spec.PreviousConsumerRef != nil &&
		spec.PreviousConsumerRef.UID != "" &&
		spec.ReuseApproval != nil &&
		spec.ReuseApproval.PreviousConsumerUID != "" &&
		spec.ReuseApproval.PreviousConsumerUID == spec.PreviousConsumerRef.UID
}

func validReuseMode(mode infrav1alpha1.ReuseMode) bool {
	return mode == infrav1alpha1.ReuseModeAdopt || mode == infrav1alpha1.ReuseModeReprovision
}
