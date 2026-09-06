// Package machineはTartMachine⇔TartHostのbinding判定をオーケストレーションするusecaseを提供する。
// domain/machineの純粋関数へKubernetes/CAPI core型を橋渡しし、判定結果のみを呼び出し元へ返す。
// 副作用(Get/List/Update)は呼び出し元のcontrollerがclient経由で行い、このpackage自体はclientを持たない。
package machine

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	machinedomain "github.com/walnuts1018/cluster-api-provider-tart/domain/machine"
)

// FindClaimedHostは、指定されたMachine UIDをconsumerRefとして持つTartHostをhosts一覧から一意に特定する。
// 一致するHostがない場合はnilを返す。複数一致した場合はmachinedomain.ErrAmbiguousClaimを返し、
// 呼び出し元はfail-closedでMachine finalizer解除やclaim更新を停止すべきである。
func FindClaimedHost(hosts []infrav1alpha1.TartHost, machineUID string) (*infrav1alpha1.TartHost, error) {
	candidates := make([]machinedomain.HostClaimCandidate, 0, len(hosts))
	for index := range hosts {
		host := &hosts[index]
		var consumerUID machinedomain.ConsumerUID
		if host.Spec.ConsumerRef != nil {
			consumerUID = machinedomain.ConsumerUID(host.Spec.ConsumerRef.UID)
		}
		candidates = append(candidates, machinedomain.HostClaimCandidate{Name: host.Name, ConsumerUID: consumerUID})
	}
	claimedName, err := machinedomain.FindClaimedHostName(candidates, machinedomain.ConsumerUID(machineUID))
	if err != nil {
		return nil, err
	}
	if claimedName == "" {
		return nil, nil
	}
	for index := range hosts {
		if hosts[index].Name == claimedName {
			return &hosts[index], nil
		}
	}
	return nil, nil
}

// deletionDrainCompletionReasonsは、CAPI core側のMachineDeletingConditionのreasonのうち、
// providerがdrain/volume detach完了とみなしてHost解放処理を進めてよいものである。
var deletionDrainCompletionReasons = []string{
	clusterv1.MachineDeletingWaitingForInfrastructureDeletionReason,
	clusterv1.MachineDeletingWaitingForBootstrapDeletionReason,
	clusterv1.MachineDeletingDeletingNodeReason,
	clusterv1.MachineDeletingDeletionCompletedReason,
}

// DeletionDrainCompleteは、CAPI Machineの削除がinfra側のHost解放処理を進めてよい段階まで進んでいるかを判定する。
func DeletionDrainComplete(capiMachine *clusterv1.Machine) bool {
	if capiMachine == nil {
		return false
	}
	condition := findCondition(capiMachine.Status.Conditions, clusterv1.MachineDeletingCondition)
	conditionTrue := condition != nil && condition.Status == metav1.ConditionTrue
	reason := ""
	if condition != nil {
		reason = condition.Reason
	}
	return machinedomain.DrainComplete(!capiMachine.DeletionTimestamp.IsZero(), conditionTrue, reason, deletionDrainCompletionReasons)
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for index := range conditions {
		if conditions[index].Type == conditionType {
			return &conditions[index]
		}
	}
	return nil
}

// IsProvisionedは、TartMachineが過去にTalos installationの完了を観測済みかを返す。
func IsProvisioned(machine *infrav1alpha1.TartMachine) bool {
	return machine.Status.Initialization.Provisioned != nil && *machine.Status.Initialization.Provisioned
}

// HasShutdownRequestは、TartMachineのReady ConditionがTalos shutdown要求済み状態かを返す。
func HasShutdownRequest(machine *infrav1alpha1.TartMachine) bool {
	condition := findCondition(machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if condition == nil {
		return false
	}
	return machinedomain.IsShutdownRequested(condition.Reason)
}

// ShutdownRequestSettledは、shutdown要求後の確認待ち時間(delay)が経過しているかを返す。
func ShutdownRequestSettled(machine *infrav1alpha1.TartMachine, delay time.Duration) bool {
	condition := findCondition(machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if condition == nil {
		return false
	}
	return machinedomain.IsShutdownSettled(condition.Reason, condition.LastTransitionTime.Time, time.Now(), delay)
}
