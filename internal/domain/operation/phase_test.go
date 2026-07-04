package operation

import (
	"errors"
	"testing"
)

func TestTransition(t *testing.T) {
	t.Parallel()

	allowed := map[Phase][]Phase{
		PhasePending:              {PhasePreparingBoot, PhaseFailed},
		PhasePreparingBoot:        {PhaseWaitingForAgent, PhaseFailed},
		PhaseWaitingForAgent:      {PhaseWriting, PhaseFailed},
		PhaseWriting:              {PhaseVerifying, PhaseFailed},
		PhaseVerifying:            {PhaseBootTrial, PhaseFailed},
		PhaseBootTrial:            {PhaseAwaitingHealth, PhaseRollingBack},
		PhaseAwaitingHealth:       {PhaseSucceeded, PhaseDistributionUpdating, PhaseRollingBack, PhaseRecoveryRequired},
		PhaseDistributionUpdating: {PhaseBootTrial, PhaseAwaitingHealth, PhaseRecoveryRequired},
		PhaseRollingBack:          {PhaseFailed, PhaseRecoveryRequired},
		PhaseSucceeded:            {},
		PhaseFailed:               {},
		PhaseRecoveryRequired:     {},
	}

	for _, from := range AllPhases() {
		for _, to := range AllPhases() {
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				t.Parallel()

				got, err := Transition(from, to)
				wantAllowed := containsPhase(allowed[from], to)
				if !wantAllowed {
					if !errors.Is(err, ErrInvalidTransition) {
						t.Fatalf("Transition() error = %v, want %v", err, ErrInvalidTransition)
					}
					return
				}
				if err != nil {
					t.Fatalf("Transition() error = %v", err)
				}
				if got != to {
					t.Fatalf("Transition() = %q, want %q", got, to)
				}
			})
		}
	}
}

func TestTransitionRejectsUnknownPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current Phase
		target  Phase
	}{
		{name: "unknown current", current: Phase("unknown"), target: PhasePending},
		{name: "unknown target", current: PhasePending, target: Phase("unknown")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Transition(tt.current, tt.target); !errors.Is(err, ErrUnknownPhase) {
				t.Fatalf("Transition() error = %v, want %v", err, ErrUnknownPhase)
			}
		})
	}
}

func TestTerminal(t *testing.T) {
	t.Parallel()

	for _, phase := range AllPhases() {
		want := phase == PhaseSucceeded || phase == PhaseFailed || phase == PhaseRecoveryRequired
		if got := phase.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %t, want %t", phase, got, want)
		}
	}
}

func containsPhase(phases []Phase, target Phase) bool {
	for _, phase := range phases {
		if phase == target {
			return true
		}
	}
	return false
}
