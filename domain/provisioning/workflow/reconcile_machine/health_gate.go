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
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
)

type healthGatePorts interface {
	PlanHealthGateRoute(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.HealthGateRouteResult, error)
	ApplyNodeHealthStatus(
		*infrastructurev1beta1.TartMachine,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.MachineStatusPatchResult, error)
	DecideProvisionHealthGate(
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.ProvisionHealthGateDecisionResult, error)
	CompleteProvision(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) error
	SetProvisionHealthPending(
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.ProvisionHealthGatePending,
	) (machineexecutionmodel.MachineStatusPatchResult, error)
	DecideUpdateHealthGate(
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.UpdateHealthGateDecisionResult, error)
	CompleteUpdate(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.TartHostOperation,
	) error
	RollbackUpdate(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) error
	ApplyUpdateTerminal(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.UpdateOperationApplyTerminal,
	) error
	PatchPlannedMachineStatus(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.MachineStatusPatchResult,
	) error
}

type healthGate struct {
	steps healthGatePorts
}

func newHealthGate(steps healthGatePorts) *healthGate {
	return &healthGate{steps: steps}
}

func (handler *healthGate) run(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) error {
	route, err := handler.steps.PlanHealthGateRoute(ctx, machine, observation)
	if err != nil {
		return err
	}
	patchResult, err := handler.handleRoute(ctx, machine, route)
	if err != nil {
		return err
	}
	return handler.steps.PatchPlannedMachineStatus(ctx, machine, patchResult)
}

func (handler *healthGate) handleRoute(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	route machineexecutionmodel.HealthGateRouteResult,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	switch route := route.(type) {
	case machineexecutionmodel.HealthGateNodeStatusRoute:
		return handler.steps.ApplyNodeHealthStatus(machine, route.Observation)
	case machineexecutionmodel.HealthGateProvisionRoute:
		return handler.applyProvision(ctx, machine, route.Operation, route.Observation)
	case machineexecutionmodel.HealthGateUpdateRoute:
		return handler.applyUpdate(ctx, machine, route.Operation, route.Observation)
	case machineexecutionmodel.HealthGateUpdateTerminalRoute:
		if err := handler.steps.ApplyUpdateTerminal(ctx, machine, machineexecutionmodel.UpdateOperationApplyTerminal(route)); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchAlreadyApplied{}, nil
	default:
		return nil, fmt.Errorf("unknown Health Gate route result: %T", route)
	}
}

func (handler *healthGate) applyProvision(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	decision, err := handler.steps.DecideProvisionHealthGate(operation, observation)
	if err != nil {
		return nil, err
	}
	switch decision := decision.(type) {
	case machineexecutionmodel.ProvisionHealthGateComplete:
		original := machine.DeepCopy()
		if err := handler.steps.CompleteProvision(ctx, machine, decision.Operation, decision.Observation); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
	case machineexecutionmodel.ProvisionHealthGatePending:
		return handler.steps.SetProvisionHealthPending(machine, decision)
	default:
		return nil, fmt.Errorf("unknown Provision health gate decision result: %T", decision)
	}
}

func (handler *healthGate) applyUpdate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	decision, err := handler.steps.DecideUpdateHealthGate(operation, observation)
	if err != nil {
		return nil, err
	}
	original := machine.DeepCopy()
	switch decision := decision.(type) {
	case machineexecutionmodel.UpdateHealthGateComplete:
		if err := handler.steps.CompleteUpdate(ctx, machine, decision.Operation); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
	case machineexecutionmodel.UpdateHealthGateRollback:
		if err := handler.steps.RollbackUpdate(ctx, machine, decision.Operation, decision.Observation); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
	default:
		return nil, fmt.Errorf("unknown Update health gate decision result: %T", decision)
	}
}
