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

package machinelifecycle

import (
	"fmt"

	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type MachineState struct {
	Provisioned  bool
	HasOperation bool
}

type ObservedState interface {
	isObservedState()
}

type ObservedActive struct{}

type ObservedDeleting struct {
	FinalizerPresent bool
}

type OperationState struct {
	Kind  operationdomain.Kind
	Phase operationdomain.Phase
}

type Readiness struct {
	Ready   bool
	Reason  string
	Message string
}

type MachineCommand interface {
	isMachineCommand()
}

type LifecycleCommand interface {
	isLifecycleCommand()
}

type OperationCommand interface {
	isOperationCommand()
}

type ProvisionCommand interface {
	isProvisionCommand()
}

type ProvisionHealthCommand interface {
	isProvisionHealthCommand()
}

type UpdateHealthCommand interface {
	isUpdateHealthCommand()
}

type CommandObserveProvisionedMachine struct{}
type CommandEnsureProvisionReference struct{}

type CommandReconcileActive struct{}
type CommandFinalizeDeleting struct{}
type CommandIgnoreDeleting struct{}

type CommandStartProvision struct{}
type CommandResumeProvisionOperation struct{}

type CommandObserveProvisionHealth struct{}

type CommandMarkProvisionFailed struct {
	Reason  string
	Message string
}

type CommandApplyUpdateTerminal struct {
	Outcome UpdateOutcome
}

type CommandObserveUpdateHealth struct{}
type CommandObserveNodeHealth struct{}

type CommandCompleteProvision struct{}

type CommandSetProvisionHealthPending struct {
	Reason  string
	Message string
}

type CommandCompleteUpdate struct{}
type CommandRollbackUpdate struct{}

type UpdateOutcome string

const (
	UpdateOutcomeSucceeded        UpdateOutcome = "Succeeded"
	UpdateOutcomeRolledBack       UpdateOutcome = "RolledBack"
	UpdateOutcomeRecoveryRequired UpdateOutcome = "RecoveryRequired"
)

func (CommandObserveProvisionedMachine) isMachineCommand() {}
func (CommandEnsureProvisionReference) isMachineCommand()  {}

func (CommandReconcileActive) isLifecycleCommand()  {}
func (CommandFinalizeDeleting) isLifecycleCommand() {}
func (CommandIgnoreDeleting) isLifecycleCommand()   {}

func (ObservedActive) isObservedState()   {}
func (ObservedDeleting) isObservedState() {}

func (CommandStartProvision) isProvisionCommand()           {}
func (CommandResumeProvisionOperation) isProvisionCommand() {}

func (CommandObserveProvisionHealth) isOperationCommand() {}
func (CommandMarkProvisionFailed) isOperationCommand()    {}
func (CommandApplyUpdateTerminal) isOperationCommand()    {}
func (CommandObserveUpdateHealth) isOperationCommand()    {}
func (CommandObserveNodeHealth) isOperationCommand()      {}

func (CommandCompleteProvision) isProvisionHealthCommand()         {}
func (CommandSetProvisionHealthPending) isProvisionHealthCommand() {}

func (CommandCompleteUpdate) isUpdateHealthCommand() {}
func (CommandRollbackUpdate) isUpdateHealthCommand() {}

func DecideMachine(state MachineState) MachineCommand {
	if state.Provisioned {
		return CommandObserveProvisionedMachine{}
	}
	return CommandEnsureProvisionReference{}
}

func DecideLifecycle(observed ObservedState) (LifecycleCommand, error) {
	switch observed := observed.(type) {
	case ObservedActive:
		return CommandReconcileActive{}, nil
	case ObservedDeleting:
		if !observed.FinalizerPresent {
			return CommandIgnoreDeleting{}, nil
		}
		return CommandFinalizeDeleting{}, nil
	default:
		return nil, fmt.Errorf("unknown TartMachine lifecycle observation: %T", observed)
	}
}

func DecideProvision(state MachineState) ProvisionCommand {
	if state.HasOperation {
		return CommandResumeProvisionOperation{}
	}
	return CommandStartProvision{}
}

func DecideOperation(machine MachineState, operation OperationState) (OperationCommand, error) {
	if operation.Phase != "" && !operation.Phase.Valid() {
		return nil, fmt.Errorf("%w: %q", operationdomain.ErrUnknownPhase, operation.Phase)
	}
	if !machine.Provisioned {
		return decideProvisionOperation(operation), nil
	}
	return decideProvisionedOperation(operation), nil
}

func DecideProvisionHealth(readiness Readiness) ProvisionHealthCommand {
	if readiness.Ready {
		return CommandCompleteProvision{}
	}
	return CommandSetProvisionHealthPending{Reason: readiness.Reason, Message: readiness.Message}
}

func DecideUpdateHealth(health machinehealthdomain.Result) UpdateHealthCommand {
	if health.Ready {
		return CommandCompleteUpdate{}
	}
	return CommandRollbackUpdate{}
}

func decideProvisionOperation(operation OperationState) OperationCommand {
	switch operation.Phase {
	case operationdomain.PhaseFailed, operationdomain.PhaseRecoveryRequired:
		return CommandMarkProvisionFailed{
			Reason:  "OperationFailed",
			Message: fmt.Sprintf("TartHostOperation finished in %s", operation.Phase),
		}
	case "",
		operationdomain.PhasePending,
		operationdomain.PhasePreparingBoot,
		operationdomain.PhaseWaitingForAgent,
		operationdomain.PhaseWriting,
		operationdomain.PhaseVerifying,
		operationdomain.PhaseBootTrial,
		operationdomain.PhaseAwaitingHealth,
		operationdomain.PhaseDistributionUpdating,
		operationdomain.PhaseRollingBack,
		operationdomain.PhaseSucceeded:
		return CommandObserveProvisionHealth{}
	}
	return CommandObserveProvisionHealth{}
}

func decideProvisionedOperation(operation OperationState) OperationCommand {
	if operation.Kind != operationdomain.KindUpdate {
		return CommandObserveNodeHealth{}
	}
	switch operation.Phase {
	case operationdomain.PhaseSucceeded:
		return CommandApplyUpdateTerminal{Outcome: UpdateOutcomeSucceeded}
	case operationdomain.PhaseFailed:
		return CommandApplyUpdateTerminal{Outcome: UpdateOutcomeRolledBack}
	case operationdomain.PhaseRecoveryRequired:
		return CommandApplyUpdateTerminal{Outcome: UpdateOutcomeRecoveryRequired}
	case operationdomain.PhaseAwaitingHealth:
		return CommandObserveUpdateHealth{}
	case "",
		operationdomain.PhasePending,
		operationdomain.PhasePreparingBoot,
		operationdomain.PhaseWaitingForAgent,
		operationdomain.PhaseWriting,
		operationdomain.PhaseVerifying,
		operationdomain.PhaseBootTrial,
		operationdomain.PhaseDistributionUpdating,
		operationdomain.PhaseRollingBack:
		return CommandObserveNodeHealth{}
	}
	return CommandObserveNodeHealth{}
}
