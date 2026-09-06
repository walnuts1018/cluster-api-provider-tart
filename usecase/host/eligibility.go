package host

import (
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

// ClassifyはHostのSpecだけから現在のallocation eligibilityを計算する。外部副作用を実行せず、HostがfreshまたはClaimedの間に設定されたreusePolicyやreuseApprovalを将来の削除承認として扱わない。これらのfieldはHostがRetainedとなりpreviousConsumerRefが存在した場合だけ有効になる。
func Classify(spec infrav1alpha1.TartHostSpec) hostdomain.Eligibility {
	if spec.ConsumerRef != nil {
		return hostdomain.Claimed
	}
	if spec.PreviousConsumerRef == nil {
		return hostdomain.Available
	}
	if spec.ReusePolicy == infrav1alpha1.ReusePolicyAllowReuse &&
		spec.ReuseApproval != nil &&
		spec.PreviousConsumerRef.UID != "" &&
		spec.ReuseApproval.PreviousConsumerUID != "" &&
		spec.ReuseApproval.PreviousConsumerUID == spec.PreviousConsumerRef.UID &&
		validReuseMode(spec.ReuseMode) {
		return hostdomain.Reusable
	}
	return hostdomain.Retained
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
