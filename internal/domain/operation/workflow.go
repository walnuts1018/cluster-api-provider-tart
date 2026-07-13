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

package operation

import (
	"errors"
	"fmt"
	"time"
)

var ErrUnknownKind = errors.New("unknown TartHostOperation kind")
var ErrUnknownCleaningPolicy = errors.New("unknown TartHostOperation cleaning policy")

type Kind string

const (
	KindProvision Kind = "Provision"
	KindUpdate    Kind = "Update"
	KindRollback  Kind = "Rollback"
	KindClean     Kind = "Clean"
	KindWipeAll   Kind = "WipeAll"
	KindRecovery  Kind = "Recovery"
)

func ParseKind(value string) (Kind, error) {
	kind := Kind(value)
	switch kind {
	case KindProvision, KindUpdate, KindRollback, KindClean, KindWipeAll, KindRecovery:
		return kind, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, value)
	}
}

type ProcessState struct {
	Kind           Kind
	Phase          Phase
	CleaningPolicy CleaningPolicy
	Deadline       time.Time
}

type ProcessInput struct {
	State ProcessState
	Now   time.Time
}

type Decision struct {
	Command Command
	Events  []Event
}

type Command interface {
	isCommand()
}

type Event interface {
	isEvent()
}

type HostCommand interface {
	isHostCommand()
}

type DeadlineOutcome interface {
	isDeadlineOutcome()
}

type CleaningPolicy string

const (
	CleaningPolicyUnspecified CleaningPolicy = ""
	CleaningPolicyRetainData  CleaningPolicy = "RetainData"
	CleaningPolicyRetainState CleaningPolicy = "RetainState"
	CleaningPolicyWipeAll     CleaningPolicy = "WipeAll"
)

type CommandInitializePending struct {
	Target Phase
}

type CommandPrepareBoot struct {
	Host HostCommand
}

type CommandObserveActive struct{}
type CommandAwaitMachineHealth struct{}

type CommandCompleteWipeAll struct {
	Host   HostCommand
	Target Phase
}

type CommandCompleteCleaning struct {
	Host   HostCommand
	Target Phase
}

type CommandHandleTerminal struct {
	Host HostCommand
}

type CommandFailDeadlineExceeded struct {
	Outcome DeadlineOutcome
}

type CommandIgnore struct{}

type HostNoop struct{}
type HostMarkProvisioning struct{}
type HostMarkUpdating struct{}
type HostMarkCleaning struct {
	Policy CleaningPolicy
}
type HostMarkAvailable struct{}
type HostMarkRetained struct{}
type HostMarkDetached struct{}
type HostMarkProvisioned struct{}
type HostMarkRecoveryRequired struct{}

type DeadlineMarkFailed struct {
	WithUpdateFailure bool
	FailedPhase       Phase
}

type DeadlineRecordBootFailure struct{}

type DeadlineTransitionFailure struct {
	FailedPhase Phase
	Target      Phase
}

type EventOperationObserved struct {
	Kind  Kind
	Phase Phase
}

type EventDeadlineExceeded struct {
	Kind     Kind
	Phase    Phase
	Deadline time.Time
}

type EventTerminalObserved struct {
	Kind  Kind
	Phase Phase
}

func (CommandInitializePending) isCommand()    {}
func (CommandPrepareBoot) isCommand()          {}
func (CommandObserveActive) isCommand()        {}
func (CommandAwaitMachineHealth) isCommand()   {}
func (CommandCompleteWipeAll) isCommand()      {}
func (CommandCompleteCleaning) isCommand()     {}
func (CommandHandleTerminal) isCommand()       {}
func (CommandFailDeadlineExceeded) isCommand() {}
func (CommandIgnore) isCommand()               {}

func (HostNoop) isHostCommand()                 {}
func (HostMarkProvisioning) isHostCommand()     {}
func (HostMarkUpdating) isHostCommand()         {}
func (HostMarkCleaning) isHostCommand()         {}
func (HostMarkAvailable) isHostCommand()        {}
func (HostMarkRetained) isHostCommand()         {}
func (HostMarkDetached) isHostCommand()         {}
func (HostMarkProvisioned) isHostCommand()      {}
func (HostMarkRecoveryRequired) isHostCommand() {}

func (DeadlineMarkFailed) isDeadlineOutcome()        {}
func (DeadlineRecordBootFailure) isDeadlineOutcome() {}
func (DeadlineTransitionFailure) isDeadlineOutcome() {}

func (EventOperationObserved) isEvent() {}
func (EventDeadlineExceeded) isEvent()  {}
func (EventTerminalObserved) isEvent()  {}

func Process(input ProcessInput) (Decision, error) {
	state := input.State
	if state.Phase != "" && !state.Phase.Valid() {
		return Decision{}, fmt.Errorf("%w: %q", ErrUnknownPhase, state.Phase)
	}
	if err := validateCleaningPolicy(state); err != nil {
		return Decision{}, err
	}
	if state.Phase.Terminal() {
		return Decision{
			Command: CommandHandleTerminal{Host: terminalHostCommand(state)},
			Events: []Event{
				EventTerminalObserved{Kind: state.Kind, Phase: state.Phase},
			},
		}, nil
	}
	if !state.Deadline.IsZero() && input.Now.After(state.Deadline) {
		return Decision{
			Command: CommandFailDeadlineExceeded{Outcome: deadlineOutcome(state)},
			Events: []Event{
				EventDeadlineExceeded{Kind: state.Kind, Phase: state.Phase, Deadline: state.Deadline},
			},
		}, nil
	}

	observed := EventOperationObserved{Kind: state.Kind, Phase: state.Phase}
	switch state.Phase {
	case "":
		return Decision{Command: CommandInitializePending{Target: PhasePending}, Events: []Event{observed}}, nil
	case PhasePending:
		return Decision{Command: CommandPrepareBoot{Host: pendingHostCommand(state)}, Events: []Event{observed}}, nil
	case PhasePreparingBoot,
		PhaseWaitingForAgent,
		PhaseWriting,
		PhaseVerifying,
		PhaseBootTrial:
		return Decision{Command: CommandObserveActive{}, Events: []Event{observed}}, nil
	case PhaseAwaitingHealth:
		switch state.Kind {
		case KindProvision, KindUpdate, KindRollback, KindRecovery:
			return Decision{Command: CommandAwaitMachineHealth{}, Events: []Event{observed}}, nil
		case KindWipeAll:
			return Decision{
				Command: CommandCompleteWipeAll{Host: HostMarkAvailable{}, Target: PhaseSucceeded},
				Events:  []Event{observed},
			}, nil
		case KindClean:
			return Decision{
				Command: CommandCompleteCleaning{Host: completedCleaningHostCommand(state), Target: PhaseSucceeded},
				Events:  []Event{observed},
			}, nil
		}
	case PhaseDistributionUpdating,
		PhaseRollingBack,
		PhaseSucceeded,
		PhaseFailed,
		PhaseRecoveryRequired:
		return Decision{Command: CommandIgnore{}, Events: []Event{observed}}, nil
	}
	return Decision{}, fmt.Errorf("%w: %q", ErrUnknownPhase, state.Phase)
}

func validateCleaningPolicy(state ProcessState) error {
	if state.Kind != KindClean {
		return nil
	}
	switch state.Phase {
	case PhasePending, PhaseAwaitingHealth:
		switch state.CleaningPolicy {
		case CleaningPolicyRetainData, CleaningPolicyRetainState:
			return nil
		case CleaningPolicyUnspecified, CleaningPolicyWipeAll:
			return fmt.Errorf("%w: %q", ErrUnknownCleaningPolicy, state.CleaningPolicy)
		}
	case PhasePreparingBoot,
		PhaseWaitingForAgent,
		PhaseWriting,
		PhaseVerifying,
		PhaseBootTrial,
		PhaseDistributionUpdating,
		PhaseRollingBack,
		PhaseSucceeded,
		PhaseFailed,
		PhaseRecoveryRequired,
		"":
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnknownPhase, state.Phase)
}

func pendingHostCommand(state ProcessState) HostCommand {
	switch state.Kind {
	case KindUpdate:
		return HostMarkUpdating{}
	case KindClean:
		return HostMarkCleaning{Policy: state.CleaningPolicy}
	case KindWipeAll:
		return HostMarkCleaning{Policy: CleaningPolicyWipeAll}
	case KindProvision, KindRollback, KindRecovery:
		return HostMarkProvisioning{}
	}
	return HostNoop{}
}

func completedCleaningHostCommand(state ProcessState) HostCommand {
	switch state.CleaningPolicy {
	case CleaningPolicyRetainData:
		return HostMarkRetained{}
	case CleaningPolicyRetainState:
		return HostMarkDetached{}
	case CleaningPolicyUnspecified, CleaningPolicyWipeAll:
		return HostNoop{}
	}
	return HostNoop{}
}

func terminalHostCommand(state ProcessState) HostCommand {
	if state.Kind != KindUpdate {
		return HostNoop{}
	}
	switch state.Phase {
	case PhaseSucceeded, PhaseFailed:
		return HostMarkProvisioned{}
	case PhaseRecoveryRequired:
		return HostMarkRecoveryRequired{}
	case PhasePending,
		PhasePreparingBoot,
		PhaseWaitingForAgent,
		PhaseWriting,
		PhaseVerifying,
		PhaseBootTrial,
		PhaseAwaitingHealth,
		PhaseDistributionUpdating,
		PhaseRollingBack,
		"":
		return HostNoop{}
	}
	return HostNoop{}
}

func deadlineOutcome(state ProcessState) DeadlineOutcome {
	if state.Kind != KindUpdate {
		return DeadlineMarkFailed{}
	}
	switch state.Phase {
	case PhaseBootTrial:
		return DeadlineRecordBootFailure{}
	case PhaseAwaitingHealth:
		return DeadlineTransitionFailure{FailedPhase: PhaseAwaitingHealth, Target: PhaseRollingBack}
	case PhaseRollingBack:
		return DeadlineTransitionFailure{FailedPhase: PhaseRollingBack, Target: PhaseRecoveryRequired}
	case PhasePending,
		PhasePreparingBoot,
		PhaseWaitingForAgent,
		PhaseWriting,
		PhaseVerifying,
		PhaseDistributionUpdating,
		PhaseSucceeded,
		PhaseFailed,
		PhaseRecoveryRequired,
		"":
		return DeadlineMarkFailed{WithUpdateFailure: true, FailedPhase: state.Phase}
	}
	return DeadlineMarkFailed{WithUpdateFailure: true, FailedPhase: state.Phase}
}
