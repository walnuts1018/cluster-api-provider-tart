// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

// DistributionLifecycleFeatureGatesはDistribution Lifecycle更新で対象Machineを受理できる範囲を表す。
type DistributionLifecycleFeatureGates struct {
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
	reader            client.Reader
	updateGates       UpdateTargetFeatureGates
	distributionGates DistributionLifecycleFeatureGates
}

// NewTargetSupportCheckerは対象判定器を生成する。
func NewTargetSupportChecker(
	reader client.Reader,
	updateGates UpdateTargetFeatureGates,
	distributionGates DistributionLifecycleFeatureGates,
) *TargetSupportChecker {
	return &TargetSupportChecker{
		reader:            reader,
		updateGates:       updateGates,
		distributionGates: distributionGates,
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

// SupportsDistributionLifecycleMachineは対象MachineがDistribution Lifecycle更新で許可されているかを返す。
func (checker *TargetSupportChecker) SupportsDistributionLifecycleMachine(
	ctx context.Context,
	machine *clusterv1.Machine,
) (bool, string, error) {
	reasons := distributionLifecycleDisabledReasons()
	return checker.supportsMachine(
		ctx,
		machine,
		checker.distributionGates.Worker,
		checker.distributionGates.MultiControlPlane,
		checker.distributionGates.SingleControlPlane,
		reasons.worker,
		reasons.multiControlPlane,
		reasons.singleControlPlane,
	)
}

// SupportsDistributionLifecycleMachineSetは対象MachineSetがDistribution Lifecycle更新で許可されているかを返す。
func (checker *TargetSupportChecker) SupportsDistributionLifecycleMachineSet(
	_ context.Context,
	machineSet *clusterv1.MachineSet,
) (bool, string, error) {
	return supportsWorkerMachineSet(
		machineSet,
		checker.distributionGates.Worker,
		distributionLifecycleDisabledReasons().worker,
	)
}

func updateTargetDisabledReasons() targetDisabledReasons {
	return targetDisabledReasons{
		worker:             "worker in-place updates are disabled by feature gate",
		multiControlPlane:  "multi control plane in-place updates are disabled by feature gate",
		singleControlPlane: "single control plane in-place updates are disabled by feature gate",
	}
}

func distributionLifecycleDisabledReasons() targetDisabledReasons {
	return targetDisabledReasons{
		worker:            "worker distribution lifecycle updates are disabled by feature gate",
		multiControlPlane: "multi control plane distribution lifecycle updates are disabled by feature gate",
		singleControlPlane: "single control plane distribution lifecycle updates are disabled by feature gate: " +
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
