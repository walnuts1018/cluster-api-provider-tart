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

// TargetSupportCheckerは対象Machine種別とfeature gateの組み合わせを判定する。
type TargetSupportChecker struct {
	reader client.Reader
	gates  UpdateTargetFeatureGates
}

// NewTargetSupportCheckerは対象判定器を生成する。
func NewTargetSupportChecker(
	reader client.Reader,
	gates UpdateTargetFeatureGates,
) *TargetSupportChecker {
	return &TargetSupportChecker{
		reader: reader,
		gates:  gates,
	}
}

// SupportsMachineは対象Machineがfeature gateで許可されているかを返す。
func (checker *TargetSupportChecker) SupportsMachine(
	ctx context.Context,
	machine *clusterv1.Machine,
) (bool, string, error) {
	if machine == nil {
		return false, "", fmt.Errorf("machine is required")
	}
	if !isControlPlaneMachine(machine) {
		if checker.gates.Worker {
			return true, "", nil
		}
		return false, "worker in-place updates are disabled by feature gate", nil
	}

	count, err := checker.controlPlaneMachineCount(ctx, machine)
	if err != nil {
		return false, "", err
	}
	if count <= 1 {
		if checker.gates.SingleControlPlane {
			return true, "", nil
		}
		return false, "single control plane in-place updates are disabled by feature gate", nil
	}
	if checker.gates.MultiControlPlane {
		return true, "", nil
	}
	return false, "multi control plane in-place updates are disabled by feature gate", nil
}

// SupportsMachineSetは対象MachineSetがfeature gateで許可されているかを返す。
func (checker *TargetSupportChecker) SupportsMachineSet(
	_ context.Context,
	machineSet *clusterv1.MachineSet,
) (bool, string, error) {
	if machineSet == nil {
		return false, "", fmt.Errorf("MachineSet is required")
	}
	if checker.gates.Worker {
		return true, "", nil
	}
	return false, "worker in-place updates are disabled by feature gate", nil
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
