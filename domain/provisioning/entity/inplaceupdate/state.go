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

package inplaceupdate

import (
	"errors"
	"fmt"

	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/host"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/slot"
)

const maxBootTrialAttempts int32 = 3

var ErrInvalidUpdateTransition = errors.New("invalid in-place update transition")

// EventはOSOnly更新状態機械へ入力できる閉じたeventである。
type Event string

const (
	EventWriteFailed       Event = "WriteFailed"
	EventVerifyFailed      Event = "VerifyFailed"
	EventBootFailed        Event = "BootFailed"
	EventMountFailed       Event = "MountFailed"
	EventNodeHealthFailed  Event = "NodeHealthFailed"
	EventTargetHealthy     Event = "TargetHealthy"
	EventRollbackHealthy   Event = "RollbackHealthy"
	EventRollbackUnhealthy Event = "RollbackUnhealthy"
)

// Stateは更新判断に必要な永続状態のprojectionである。
type State struct {
	Phase      operationdomain.Phase
	ActiveSlot slotdomain.Slot
	TargetSlot slotdomain.Slot
	Attempt    int32
}

// Decisionはevent適用後に永続化すべき状態を表す。
type Decision struct {
	Phase           operationdomain.Phase
	BootSlot        slotdomain.Slot
	ActiveSlot      slotdomain.Slot
	Attempt         int32
	HostPhase       hostdomain.Phase
	MachineReady    bool
	FailureRetained bool
}

// Transitionはboot試行、Commit、Rollbackの次状態を副作用なしで決定する。
func Transition(state State, event Event) (Decision, error) {
	if err := validateState(state); err != nil {
		return Decision{}, err
	}
	base := Decision{
		Phase:      state.Phase,
		ActiveSlot: state.ActiveSlot,
		Attempt:    state.Attempt,
		HostPhase:  hostdomain.PhaseUpdating,
	}

	switch event {
	case EventWriteFailed:
		if state.Phase != operationdomain.PhaseWriting {
			return Decision{}, invalidEvent(state, event)
		}
		return rollbackDecision(base, state)
	case EventVerifyFailed:
		if state.Phase != operationdomain.PhaseVerifying {
			return Decision{}, invalidEvent(state, event)
		}
		return rollbackDecision(base, state)
	case EventMountFailed, EventNodeHealthFailed:
		if state.Phase != operationdomain.PhaseAwaitingHealth {
			return Decision{}, invalidEvent(state, event)
		}
		return rollbackDecision(base, state)
	case EventBootFailed:
		if state.Phase != operationdomain.PhaseBootTrial {
			return Decision{}, invalidEvent(state, event)
		}
		base.Attempt++
		if base.Attempt < maxBootTrialAttempts {
			base.BootSlot = state.TargetSlot
			return base, nil
		}
		return rollbackDecision(base, state)
	case EventTargetHealthy:
		if state.Phase != operationdomain.PhaseAwaitingHealth {
			return Decision{}, invalidEvent(state, event)
		}
		phase, err := operationdomain.Transition(state.Phase, operationdomain.PhaseSucceeded)
		if err != nil {
			return Decision{}, err
		}
		base.Phase = phase
		base.BootSlot = state.TargetSlot
		base.ActiveSlot = state.TargetSlot
		base.HostPhase = hostdomain.PhaseProvisioned
		base.MachineReady = true
		return base, nil
	case EventRollbackHealthy:
		if state.Phase != operationdomain.PhaseRollingBack {
			return Decision{}, invalidEvent(state, event)
		}
		phase, err := operationdomain.Transition(state.Phase, operationdomain.PhaseFailed)
		if err != nil {
			return Decision{}, err
		}
		base.Phase = phase
		base.BootSlot = state.ActiveSlot
		base.HostPhase = hostdomain.PhaseProvisioned
		base.MachineReady = true
		base.FailureRetained = true
		return base, nil
	case EventRollbackUnhealthy:
		if state.Phase != operationdomain.PhaseRollingBack {
			return Decision{}, invalidEvent(state, event)
		}
		phase, err := operationdomain.Transition(state.Phase, operationdomain.PhaseRecoveryRequired)
		if err != nil {
			return Decision{}, err
		}
		base.Phase = phase
		base.BootSlot = state.ActiveSlot
		base.HostPhase = hostdomain.PhaseRecoveryRequired
		base.FailureRetained = true
		return base, nil
	default:
		return Decision{}, invalidEvent(state, event)
	}
}

func rollbackDecision(base Decision, state State) (Decision, error) {
	phase, err := operationdomain.Transition(state.Phase, operationdomain.PhaseRollingBack)
	if err != nil {
		return Decision{}, err
	}
	base.Phase = phase
	base.BootSlot = state.ActiveSlot
	base.FailureRetained = true
	return base, nil
}

func validateState(state State) error {
	if !state.Phase.Valid() || state.Phase.Terminal() {
		return fmt.Errorf("%w: phase %q", ErrInvalidUpdateTransition, state.Phase)
	}
	if state.Attempt < 0 || state.Attempt > maxBootTrialAttempts {
		return fmt.Errorf("%w: attempt %d", ErrInvalidUpdateTransition, state.Attempt)
	}
	inactive, present := state.ActiveSlot.Inactive().Value().Value()
	if !present || inactive != state.TargetSlot {
		return fmt.Errorf(
			"%w: target slot %q is not inactive for %q",
			ErrInvalidUpdateTransition,
			state.TargetSlot,
			state.ActiveSlot,
		)
	}
	return nil
}

func invalidEvent(state State, event Event) error {
	return fmt.Errorf("%w: event %q in phase %q", ErrInvalidUpdateTransition, event, state.Phase)
}
