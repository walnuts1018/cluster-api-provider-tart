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

type WorkflowInput struct {
	Kind     Kind
	Phase    Phase
	Deadline time.Time
	Now      time.Time
}

type Decision struct {
	Step   Step
	Events []Event
}

type Step interface {
	isStep()
}

type Event interface {
	isEvent()
}

type StepInitializePending struct{}
type StepPrepareBoot struct{}
type StepObserveActive struct{}
type StepAwaitMachineHealth struct{}
type StepCompleteWipeAll struct{}
type StepCompleteCleaning struct{}
type StepHandleTerminal struct{ Phase Phase }
type StepFailDeadlineExceeded struct{}
type StepIgnore struct{}

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

func (StepInitializePending) isStep()    {}
func (StepPrepareBoot) isStep()          {}
func (StepObserveActive) isStep()        {}
func (StepAwaitMachineHealth) isStep()   {}
func (StepCompleteWipeAll) isStep()      {}
func (StepCompleteCleaning) isStep()     {}
func (StepHandleTerminal) isStep()       {}
func (StepFailDeadlineExceeded) isStep() {}
func (StepIgnore) isStep()               {}

func (EventOperationObserved) isEvent() {}
func (EventDeadlineExceeded) isEvent()  {}
func (EventTerminalObserved) isEvent()  {}

func DecideNextStep(input WorkflowInput) (Decision, error) {
	if input.Phase != "" && !input.Phase.Valid() {
		return Decision{}, fmt.Errorf("%w: %q", ErrUnknownPhase, input.Phase)
	}
	if input.Phase.Terminal() {
		return Decision{
			Step: StepHandleTerminal{Phase: input.Phase},
			Events: []Event{
				EventTerminalObserved{Kind: input.Kind, Phase: input.Phase},
			},
		}, nil
	}
	if !input.Deadline.IsZero() && input.Now.After(input.Deadline) {
		return Decision{
			Step: StepFailDeadlineExceeded{},
			Events: []Event{
				EventDeadlineExceeded{Kind: input.Kind, Phase: input.Phase, Deadline: input.Deadline},
			},
		}, nil
	}

	observed := EventOperationObserved{Kind: input.Kind, Phase: input.Phase}
	switch input.Phase {
	case "":
		return Decision{Step: StepInitializePending{}, Events: []Event{observed}}, nil
	case PhasePending:
		return Decision{Step: StepPrepareBoot{}, Events: []Event{observed}}, nil
	case PhasePreparingBoot,
		PhaseWaitingForAgent,
		PhaseWriting,
		PhaseVerifying,
		PhaseBootTrial:
		return Decision{Step: StepObserveActive{}, Events: []Event{observed}}, nil
	case PhaseAwaitingHealth:
		switch input.Kind {
		case KindProvision, KindUpdate, KindRollback, KindRecovery:
			return Decision{Step: StepAwaitMachineHealth{}, Events: []Event{observed}}, nil
		case KindWipeAll:
			return Decision{Step: StepCompleteWipeAll{}, Events: []Event{observed}}, nil
		case KindClean:
			return Decision{Step: StepCompleteCleaning{}, Events: []Event{observed}}, nil
		}
	case PhaseDistributionUpdating,
		PhaseRollingBack,
		PhaseSucceeded,
		PhaseFailed,
		PhaseRecoveryRequired:
		return Decision{Step: StepIgnore{}, Events: []Event{observed}}, nil
	}
	return Decision{}, fmt.Errorf("%w: %q", ErrUnknownPhase, input.Phase)
}
