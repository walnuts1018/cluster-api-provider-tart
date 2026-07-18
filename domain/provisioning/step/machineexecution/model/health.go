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

package machineexecution

import (
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinelifecycle"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
)

type NodeHealthResult interface {
	isNodeHealthResult()
}

type NodeHealthObserved struct {
	Observation machinehealthdomain.NodeObservation
}

type NodeHealthUnavailable struct{}

type HealthGateRouteResult interface {
	isHealthGateRouteResult()
}

type HealthGateNodeStatusRoute struct {
	Observation machinehealthdomain.NodeObservation
}

type HealthGateProvisionRoute struct {
	Operation   *infrastructurev1beta1.TartHostOperation
	Observation machinehealthdomain.NodeObservation
}

type HealthGateUpdateRoute struct {
	Operation   *infrastructurev1beta1.TartHostOperation
	Observation machinehealthdomain.NodeObservation
}

type HealthGateUpdateTerminalRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Outcome   machinelifecycledomain.UpdateOutcome
}

type UpdateHealthGateDecisionResult interface {
	isUpdateHealthGateDecisionResult()
}

type UpdateHealthGateComplete struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type UpdateHealthGateRollback struct {
	Operation   *infrastructurev1beta1.TartHostOperation
	Observation machinehealthdomain.NodeObservation
}

type MachineStatusPatchResult interface {
	isMachineStatusPatchResult()
}

type MachineStatusPatchRequired struct {
	Original *infrastructurev1beta1.TartMachine
}

type MachineStatusPatchAlreadyApplied struct{}

func (NodeHealthObserved) isNodeHealthResult()    {}
func (NodeHealthUnavailable) isNodeHealthResult() {}

func (HealthGateNodeStatusRoute) isHealthGateRouteResult()     {}
func (HealthGateProvisionRoute) isHealthGateRouteResult()      {}
func (HealthGateUpdateRoute) isHealthGateRouteResult()         {}
func (HealthGateUpdateTerminalRoute) isHealthGateRouteResult() {}

func (UpdateHealthGateComplete) isUpdateHealthGateDecisionResult() {}
func (UpdateHealthGateRollback) isUpdateHealthGateDecisionResult() {}

func (MachineStatusPatchRequired) isMachineStatusPatchResult()       {}
func (MachineStatusPatchAlreadyApplied) isMachineStatusPatchResult() {}
