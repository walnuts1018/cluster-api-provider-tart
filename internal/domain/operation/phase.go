package operation

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownPhase      = errors.New("unknown TartHostOperation phase")
	ErrInvalidTransition = errors.New("invalid TartHostOperation phase transition")
)

type Phase string

const (
	PhasePending              Phase = "Pending"
	PhasePreparingBoot        Phase = "PreparingBoot"
	PhaseWaitingForAgent      Phase = "WaitingForAgent"
	PhaseWriting              Phase = "Writing"
	PhaseVerifying            Phase = "Verifying"
	PhaseBootTrial            Phase = "BootTrial"
	PhaseAwaitingHealth       Phase = "AwaitingHealth"
	PhaseDistributionUpdating Phase = "DistributionUpdating"
	PhaseRollingBack          Phase = "RollingBack"
	PhaseSucceeded            Phase = "Succeeded"
	PhaseFailed               Phase = "Failed"
	PhaseRecoveryRequired     Phase = "RecoveryRequired"
)

var allPhases = []Phase{
	PhasePending,
	PhasePreparingBoot,
	PhaseWaitingForAgent,
	PhaseWriting,
	PhaseVerifying,
	PhaseBootTrial,
	PhaseAwaitingHealth,
	PhaseDistributionUpdating,
	PhaseRollingBack,
	PhaseSucceeded,
	PhaseFailed,
	PhaseRecoveryRequired,
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
	case PhasePending,
		PhasePreparingBoot,
		PhaseWaitingForAgent,
		PhaseWriting,
		PhaseVerifying,
		PhaseBootTrial,
		PhaseAwaitingHealth,
		PhaseDistributionUpdating,
		PhaseRollingBack,
		PhaseSucceeded,
		PhaseFailed,
		PhaseRecoveryRequired:
		return true
	case "":
		return false
	}
	return false
}

func (p Phase) Terminal() bool {
	switch p {
	case PhaseSucceeded, PhaseFailed, PhaseRecoveryRequired:
		return true
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
		return false
	}
	return false
}

func Transition(current, target Phase) (Phase, error) {
	if !current.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownPhase, current)
	}
	if !target.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownPhase, target)
	}
	if !allowedTransition(current, target) {
		return "", fmt.Errorf("%w: %q to %q", ErrInvalidTransition, current, target)
	}
	return target, nil
}

func allowedTransition(current, target Phase) bool {
	switch current {
	case PhasePending:
		return target == PhasePreparingBoot || target == PhaseFailed
	case PhasePreparingBoot:
		return target == PhaseWaitingForAgent || target == PhaseFailed
	case PhaseWaitingForAgent:
		return target == PhaseWriting || target == PhaseFailed
	case PhaseWriting:
		return target == PhaseVerifying || target == PhaseFailed
	case PhaseVerifying:
		return target == PhaseBootTrial || target == PhaseFailed
	case PhaseBootTrial:
		return target == PhaseAwaitingHealth || target == PhaseRollingBack
	case PhaseAwaitingHealth:
		return target == PhaseSucceeded ||
			target == PhaseDistributionUpdating ||
			target == PhaseRollingBack ||
			target == PhaseRecoveryRequired
	case PhaseDistributionUpdating:
		return target == PhaseBootTrial ||
			target == PhaseAwaitingHealth ||
			target == PhaseRecoveryRequired
	case PhaseRollingBack:
		return target == PhaseFailed || target == PhaseRecoveryRequired
	case PhaseSucceeded, PhaseFailed, PhaseRecoveryRequired:
		return false
	case "":
		return false
	}
	return false
}
