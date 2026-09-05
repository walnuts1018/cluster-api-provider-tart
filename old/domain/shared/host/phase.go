package host

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownPhase      = errors.New("unknown TartHost phase")
	ErrInvalidTransition = errors.New("invalid TartHost phase transition")
	ErrInvalidState      = errors.New("invalid TartHost state")
)

type Phase string

const (
	PhaseAvailable        Phase = "Available"
	PhaseReserved         Phase = "Reserved"
	PhaseProvisioning     Phase = "Provisioning"
	PhaseProvisioned      Phase = "Provisioned"
	PhaseUpdating         Phase = "Updating"
	PhaseCleaning         Phase = "Cleaning"
	PhaseRetained         Phase = "Retained"
	PhaseDetached         Phase = "Detached"
	PhaseRecoveryRequired Phase = "RecoveryRequired"
	PhaseError            Phase = "Error"
)

var allPhases = []Phase{
	PhaseAvailable,
	PhaseReserved,
	PhaseProvisioning,
	PhaseProvisioned,
	PhaseUpdating,
	PhaseCleaning,
	PhaseRetained,
	PhaseDetached,
	PhaseRecoveryRequired,
	PhaseError,
}

func AllPhases() []Phase {
	return append([]Phase(nil), allPhases...)
}

func ParsePhase(value string) (Phase, error) {
	phase := Phase(value)
	if !phase.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownPhase, value)
	}
	return phase, nil
}

func (p Phase) Valid() bool {
	switch p {
	case PhaseAvailable,
		PhaseReserved,
		PhaseProvisioning,
		PhaseProvisioned,
		PhaseUpdating,
		PhaseCleaning,
		PhaseRetained,
		PhaseDetached,
		PhaseRecoveryRequired,
		PhaseError:
		return true
	case "":
		return false
	}
	return false
}

func (p Phase) Stable() bool {
	switch p {
	case PhaseAvailable, PhaseProvisioned, PhaseRetained, PhaseDetached:
		return true
	case PhaseReserved,
		PhaseProvisioning,
		PhaseUpdating,
		PhaseCleaning,
		PhaseRecoveryRequired,
		PhaseError,
		"":
		return false
	}
	return false
}

type State struct {
	phase           Phase
	lastStablePhase Phase
}

func NewState(phase, lastStablePhase Phase) (State, error) {
	if !phase.Valid() {
		return State{}, fmt.Errorf("%w: %q", ErrUnknownPhase, phase)
	}
	if phase.Stable() {
		if lastStablePhase != "" && lastStablePhase != phase {
			return State{}, fmt.Errorf("%w: stable phase %q conflicts with last stable phase %q", ErrInvalidState, phase, lastStablePhase)
		}
		return State{phase: phase, lastStablePhase: phase}, nil
	}
	if !lastStablePhase.Stable() {
		return State{}, fmt.Errorf("%w: phase %q requires a stable last phase", ErrInvalidState, phase)
	}
	return State{phase: phase, lastStablePhase: lastStablePhase}, nil
}

func (s State) Phase() Phase {
	return s.phase
}

func (s State) LastStablePhase() Phase {
	return s.lastStablePhase
}

func (s State) Transition(target Phase) (State, error) {
	if !s.phase.Valid() || !s.lastStablePhase.Stable() {
		return State{}, fmt.Errorf("%w: phase %q with last stable phase %q", ErrInvalidState, s.phase, s.lastStablePhase)
	}
	if !target.Valid() {
		return State{}, fmt.Errorf("%w: %q", ErrUnknownPhase, target)
	}
	if !allowedHostTransition(s, target) {
		return State{}, fmt.Errorf("%w: %q to %q", ErrInvalidTransition, s.phase, target)
	}
	if target.Stable() {
		return State{phase: target, lastStablePhase: target}, nil
	}
	return State{phase: target, lastStablePhase: s.lastStablePhase}, nil
}

func allowedHostTransition(state State, target Phase) bool {
	switch state.phase {
	case PhaseAvailable:
		return target == PhaseReserved || target == PhaseCleaning || target == PhaseError
	case PhaseReserved:
		return target == PhaseProvisioning || target == PhaseProvisioned || target == PhaseCleaning || target == PhaseError
	case PhaseProvisioning:
		return target == PhaseProvisioned || target == PhaseCleaning || target == PhaseError
	case PhaseProvisioned:
		return target == PhaseUpdating || target == PhaseCleaning || target == PhaseDetached || target == PhaseError
	case PhaseUpdating:
		return target == PhaseProvisioned || target == PhaseRecoveryRequired || target == PhaseError
	case PhaseCleaning:
		return target == PhaseAvailable || target == PhaseRetained || target == PhaseDetached || target == PhaseError
	case PhaseRetained:
		return target == PhaseCleaning
	case PhaseDetached:
		return target == PhaseCleaning || target == PhaseRecoveryRequired
	case PhaseRecoveryRequired:
		return target == PhaseUpdating || target == PhaseCleaning
	case PhaseError:
		return target == state.lastStablePhase || target == PhaseCleaning
	case "":
		return false
	}
	return false
}
