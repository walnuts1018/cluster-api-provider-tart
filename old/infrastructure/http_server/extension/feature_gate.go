package extension

import (
	"context"
	"fmt"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateTargetFeatureGatesはRuntime Extensionが対象Machineを受理できる範囲を表す。
type UpdateTargetFeatureGates struct {
	Worker             bool
	MultiControlPlane  bool
	SingleControlPlane bool
}

// NodeLifecycleFeatureGatesはNode Lifecycle Engine更新で対象Machineを受理できる範囲を表す。
type NodeLifecycleFeatureGates struct {
	Worker             bool
	MultiControlPlane  bool
	SingleControlPlane bool
}

type targetDisabledReasons struct {
	worker             string
	multiControlPlane  string
	singleControlPlane string
}

// TargetSupportCheckerは対象Machine種別とfeature gateの組み合わせを判定する。
type TargetSupportChecker struct {
	reader             client.Reader
	updateGates        UpdateTargetFeatureGates
	nodeLifecycleGates NodeLifecycleFeatureGates
}

// NewTargetSupportCheckerは対象判定器を生成する。
func NewTargetSupportChecker(
	reader client.Reader,
	updateGates UpdateTargetFeatureGates,
	nodeLifecycleGates NodeLifecycleFeatureGates,
) *TargetSupportChecker {
	return &TargetSupportChecker{
		reader:             reader,
		updateGates:        updateGates,
		nodeLifecycleGates: nodeLifecycleGates,
	}
}

// SupportsMachineは対象Machineがfeature gateで許可されているかを返す。
func (checker *TargetSupportChecker) SupportsMachine(
	ctx context.Context,
	machine *clusterv1.Machine,
) (bool, string, error) {
	reasons := updateTargetDisabledReasons()
	return checker.supportsMachine(
		ctx,
		machine,
		checker.updateGates.Worker,
		checker.updateGates.MultiControlPlane,
		checker.updateGates.SingleControlPlane,
		reasons.worker,
		reasons.multiControlPlane,
		reasons.singleControlPlane,
	)
}

// SupportsMachineSetは対象MachineSetがfeature gateで許可されているかを返す。
func (checker *TargetSupportChecker) SupportsMachineSet(
	_ context.Context,
	machineSet *clusterv1.MachineSet,
) (bool, string, error) {
	return supportsWorkerMachineSet(machineSet, checker.updateGates.Worker, updateTargetDisabledReasons().worker)
}

// SupportsNodeLifecycleMachineは対象MachineがNode Lifecycle Engine更新で許可されているかを返す。
func (checker *TargetSupportChecker) SupportsNodeLifecycleMachine(
	ctx context.Context,
	machine *clusterv1.Machine,
) (bool, string, error) {
	reasons := nodeLifecycleDisabledReasons()
	return checker.supportsMachine(
		ctx,
		machine,
		checker.nodeLifecycleGates.Worker,
		checker.nodeLifecycleGates.MultiControlPlane,
		checker.nodeLifecycleGates.SingleControlPlane,
		reasons.worker,
		reasons.multiControlPlane,
		reasons.singleControlPlane,
	)
}

// SupportsNodeLifecycleMachineSetは対象MachineSetがNode Lifecycle Engine更新で許可されているかを返す。
func (checker *TargetSupportChecker) SupportsNodeLifecycleMachineSet(
	_ context.Context,
	machineSet *clusterv1.MachineSet,
) (bool, string, error) {
	return supportsWorkerMachineSet(
		machineSet,
		checker.nodeLifecycleGates.Worker,
		nodeLifecycleDisabledReasons().worker,
	)
}

func updateTargetDisabledReasons() targetDisabledReasons {
	return targetDisabledReasons{
		worker:             "worker in-place updates are disabled by feature gate",
		multiControlPlane:  "multi control plane in-place updates are disabled by feature gate",
		singleControlPlane: "single control plane in-place updates are disabled by feature gate",
	}
}

func nodeLifecycleDisabledReasons() targetDisabledReasons {
	return targetDisabledReasons{
		worker:            "worker node lifecycle updates are disabled by feature gate",
		multiControlPlane: "multi control plane node lifecycle updates are disabled by feature gate",
		singleControlPlane: "single control plane node lifecycle updates are disabled by feature gate: " +
			"Experimental while management API outage E2E pending",
	}
}

func (checker *TargetSupportChecker) supportsMachine(
	ctx context.Context,
	machine *clusterv1.Machine,
	workerEnabled bool,
	multiControlPlaneEnabled bool,
	singleControlPlaneEnabled bool,
	workerReason string,
	multiControlPlaneReason string,
	singleControlPlaneReason string,
) (bool, string, error) {
	if machine == nil {
		return false, "", fmt.Errorf("machine is required")
	}
	if !isControlPlaneMachine(machine) {
		if workerEnabled {
			return true, "", nil
		}
		return false, workerReason, nil
	}

	count, err := checker.controlPlaneMachineCount(ctx, machine)
	if err != nil {
		return false, "", err
	}
	if count <= 1 {
		if singleControlPlaneEnabled {
			return true, "", nil
		}
		return false, singleControlPlaneReason, nil
	}
	if multiControlPlaneEnabled {
		return true, "", nil
	}
	return false, multiControlPlaneReason, nil
}

func supportsWorkerMachineSet(
	machineSet *clusterv1.MachineSet,
	workerEnabled bool,
	disabledReason string,
) (bool, string, error) {
	if machineSet == nil {
		return false, "", fmt.Errorf("MachineSet is required")
	}
	if workerEnabled {
		return true, "", nil
	}
	return false, disabledReason, nil
}

func (checker *TargetSupportChecker) controlPlaneMachineCount(
	ctx context.Context,
	machine *clusterv1.Machine,
) (int, error) {
	if checker.reader == nil {
		return 0, fmt.Errorf("client reader is required for control plane gate evaluation")
	}
	if machine.Namespace == "" || machine.Spec.ClusterName == "" {
		return 0, fmt.Errorf("control plane Machine namespace and clusterName are required")
	}

	machines := &clusterv1.MachineList{}
	if err := checker.reader.List(
		ctx,
		machines,
		client.InNamespace(machine.Namespace),
		client.MatchingLabels{
			clusterv1.ClusterNameLabel:         machine.Spec.ClusterName,
			clusterv1.MachineControlPlaneLabel: "",
		},
	); err != nil {
		return 0, fmt.Errorf("list control plane Machines: %w", err)
	}
	return len(machines.Items), nil
}

func isControlPlaneMachine(machine *clusterv1.Machine) bool {
	if machine == nil {
		return false
	}
	_, ok := machine.Labels[clusterv1.MachineControlPlaneLabel]
	return ok
}
