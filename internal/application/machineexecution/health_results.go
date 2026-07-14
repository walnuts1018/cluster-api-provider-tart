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
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

type nodeHealthResult interface {
	isNodeHealthResult()
}

type nodeHealthObserved struct {
	Observation machinehealthdomain.NodeObservation
}

type nodeHealthUnavailable struct{}

type healthGateRouteResult interface {
	isHealthGateRouteResult()
}

type healthGateNodeStatusRoute struct {
	Observation machinehealthdomain.NodeObservation
}

type healthGateProvisionRoute struct {
	Operation   *infrastructurev1beta1.TartHostOperation
	Observation machinehealthdomain.NodeObservation
}

type healthGateUpdateRoute struct {
	Operation   *infrastructurev1beta1.TartHostOperation
	Observation machinehealthdomain.NodeObservation
}

type healthGateUpdateTerminalRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Outcome   machinelifecycledomain.UpdateOutcome
}

type updateHealthGateDecisionResult interface {
	isUpdateHealthGateDecisionResult()
}

type updateHealthGateComplete struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type updateHealthGateRollback struct {
	Operation   *infrastructurev1beta1.TartHostOperation
	Observation machinehealthdomain.NodeObservation
}

type updateHealthGateEffectResult interface {
	isUpdateHealthGateEffectResult()
}

type updateHealthGateCompleted struct{}

type updateHealthGateRollbackStarted struct{}

type machineStatusPatchResult interface {
	isMachineStatusPatchResult()
}

type machineStatusPatchRequired struct {
	Original *infrastructurev1beta1.TartMachine
}

type machineStatusPatchAlreadyApplied struct{}

func (nodeHealthObserved) isNodeHealthResult()    {}
func (nodeHealthUnavailable) isNodeHealthResult() {}

func (healthGateNodeStatusRoute) isHealthGateRouteResult()     {}
func (healthGateProvisionRoute) isHealthGateRouteResult()      {}
func (healthGateUpdateRoute) isHealthGateRouteResult()         {}
func (healthGateUpdateTerminalRoute) isHealthGateRouteResult() {}

func (updateHealthGateComplete) isUpdateHealthGateDecisionResult() {}
func (updateHealthGateRollback) isUpdateHealthGateDecisionResult() {}

func (updateHealthGateCompleted) isUpdateHealthGateEffectResult()       {}
func (updateHealthGateRollbackStarted) isUpdateHealthGateEffectResult() {}

func (machineStatusPatchRequired) isMachineStatusPatchResult()       {}
func (machineStatusPatchAlreadyApplied) isMachineStatusPatchResult() {}
