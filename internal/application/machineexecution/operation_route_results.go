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
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

type provisioningOperationRouteResult interface {
	isProvisioningOperationRouteResult()
}

type provisioningOperationHealthRoute struct{}

type provisioningOperationFailedRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Reason    string
}

func (provisioningOperationHealthRoute) isProvisioningOperationRouteResult() {}
func (provisioningOperationFailedRoute) isProvisioningOperationRouteResult() {}

type provisionedOperationRouteResult interface {
	isProvisionedOperationRouteResult()
}

type provisionedOperationUpdateHealthRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type provisionedOperationNodeHealthRoute struct{}

type provisionedOperationUpdateTerminalRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Outcome   machinelifecycledomain.UpdateOutcome
}

func (provisionedOperationUpdateHealthRoute) isProvisionedOperationRouteResult()   {}
func (provisionedOperationNodeHealthRoute) isProvisionedOperationRouteResult()     {}
func (provisionedOperationUpdateTerminalRoute) isProvisionedOperationRouteResult() {}

func decideProvisioningOperationRouteStep(
	operation *infrastructurev1beta1.TartHostOperation,
) (provisioningOperationRouteResult, error) {
	command, err := operationCommand(false, operation)
	if err != nil {
		return nil, err
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandMarkProvisionFailed:
		return provisioningOperationFailedRoute{Operation: operation, Reason: command.Reason}, nil
	case machinelifecycledomain.CommandObserveProvisionHealth:
		return provisioningOperationHealthRoute{}, nil
	default:
		return nil, fmt.Errorf("unexpected provisioning TartMachine command: %T", command)
	}
}

func decideProvisionedOperationRouteStep(
	operation *infrastructurev1beta1.TartHostOperation,
) (provisionedOperationRouteResult, error) {
	command, err := operationCommand(true, operation)
	if err != nil {
		return nil, err
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandObserveUpdateHealth:
		return provisionedOperationUpdateHealthRoute{Operation: operation}, nil
	case machinelifecycledomain.CommandObserveNodeHealth:
		return provisionedOperationNodeHealthRoute{}, nil
	case machinelifecycledomain.CommandApplyUpdateTerminal:
		return provisionedOperationUpdateTerminalRoute{Operation: operation, Outcome: command.Outcome}, nil
	default:
		return nil, fmt.Errorf("unexpected provisioned TartMachine command: %T", command)
	}
}
