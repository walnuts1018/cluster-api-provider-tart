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

package step

import (
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

func DecideProvisionedHealthGateRoute(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (model.HealthGateRouteResult, error) {
	route, err := DecideOperationRoute(OperationProvisioned{}, operation)
	if err != nil {
		return nil, fmt.Errorf("decide Update health gate: %w", err)
	}
	switch route := route.(type) {
	case OperationUpdateHealthRoute:
		return model.HealthGateUpdateRoute{Operation: route.Operation, Observation: observation}, nil
	case OperationNodeHealthRoute:
		return model.HealthGateNodeStatusRoute{Observation: observation}, nil
	case OperationUpdateTerminalRoute:
		return model.HealthGateUpdateTerminalRoute{Operation: route.Operation, Outcome: route.Outcome}, nil
	default:
		return nil, fmt.Errorf("unknown provisioned TartMachine route: %T", route)
	}
}

func DecideUpdateHealthGate(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (model.UpdateHealthGateDecisionResult, error) {
	healthCommand := machinelifecycledomain.DecideUpdateHealth(machinehealthdomain.EvaluateNode(observation))
	switch healthCommand.(type) {
	case machinelifecycledomain.CommandCompleteUpdate:
		return model.UpdateHealthGateComplete{Operation: operation}, nil
	case machinelifecycledomain.CommandRollbackUpdate:
		return model.UpdateHealthGateRollback{Operation: operation, Observation: observation}, nil
	default:
		return nil, fmt.Errorf("unknown Update health command: %T", healthCommand)
	}
}

func DecideProvisionHealthGate(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (model.ProvisionHealthGateDecisionResult, error) {
	readiness := appprovisioning.EvaluateReadiness(operation, observation)
	command := machinelifecycledomain.DecideProvisionHealth(machinelifecycledomain.Readiness{
		Ready:   readiness.Ready,
		Reason:  readiness.Reason,
		Message: readiness.Message,
	})
	switch command := command.(type) {
	case machinelifecycledomain.CommandCompleteProvision:
		return model.ProvisionHealthGateComplete{Operation: operation, Observation: observation}, nil
	case machinelifecycledomain.CommandSetProvisionHealthPending:
		return model.ProvisionHealthGatePending{Reason: command.Reason, Message: command.Message}, nil
	default:
		return nil, fmt.Errorf("unknown Provision health command: %T", command)
	}
}
