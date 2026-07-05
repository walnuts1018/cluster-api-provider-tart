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

package host

import (
	"errors"
	"slices"
	"testing"
)

func TestStateTransition(t *testing.T) {
	t.Parallel()

	allowed := map[Phase][]Phase{
		PhaseAvailable:        {PhaseReserved, PhaseError},
		PhaseReserved:         {PhaseProvisioning, PhaseCleaning, PhaseError},
		PhaseProvisioning:     {PhaseProvisioned, PhaseCleaning, PhaseError},
		PhaseProvisioned:      {PhaseUpdating, PhaseCleaning, PhaseDetached, PhaseError},
		PhaseUpdating:         {PhaseProvisioned, PhaseRecoveryRequired, PhaseError},
		PhaseCleaning:         {PhaseAvailable, PhaseRetained, PhaseDetached, PhaseError},
		PhaseRetained:         {PhaseCleaning},
		PhaseDetached:         {PhaseCleaning, PhaseRecoveryRequired},
		PhaseRecoveryRequired: {PhaseUpdating, PhaseCleaning},
		PhaseError:            {PhaseProvisioned, PhaseCleaning},
	}

	for _, from := range AllPhases() {
		for _, to := range AllPhases() {
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				t.Parallel()

				lastStable := PhaseProvisioned
				if from.Stable() {
					lastStable = from
				}
				state, err := NewState(from, lastStable)
				if err != nil {
					t.Fatalf("NewState() error = %v", err)
				}

				got, err := state.Transition(to)
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
				if got.Phase() != to {
					t.Fatalf("Transition().Phase() = %q, want %q", got.Phase(), to)
				}
				if to.Stable() && got.LastStablePhase() != to {
					t.Fatalf("Transition().LastStablePhase() = %q, want %q", got.LastStablePhase(), to)
				}
			})
		}
	}
}

func TestNewStateValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		phase      Phase
		lastStable Phase
		wantErr    error
	}{
		{name: "stable phase determines last stable phase", phase: PhaseAvailable},
		{name: "transient phase accepts stable phase", phase: PhaseReserved, lastStable: PhaseAvailable},
		{name: "unknown phase", phase: Phase("unknown"), lastStable: PhaseAvailable, wantErr: ErrUnknownPhase},
		{name: "stable phase conflicts with last stable phase", phase: PhaseAvailable, lastStable: PhaseProvisioned, wantErr: ErrInvalidState},
		{name: "transient phase requires last stable phase", phase: PhaseUpdating, wantErr: ErrInvalidState},
		{name: "transient phase rejects transient last phase", phase: PhaseUpdating, lastStable: PhaseReserved, wantErr: ErrInvalidState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewState(tt.phase, tt.lastStable)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewState() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewState() error = %v", err)
			}
			if got.Phase() != tt.phase {
				t.Fatalf("NewState().Phase() = %q, want %q", got.Phase(), tt.phase)
			}
		})
	}
}

func TestStateTransitionRejectsUnknownTarget(t *testing.T) {
	t.Parallel()

	state, err := NewState(PhaseAvailable, "")
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	if _, err := state.Transition(Phase("unknown")); !errors.Is(err, ErrUnknownPhase) {
		t.Fatalf("Transition() error = %v, want %v", err, ErrUnknownPhase)
	}
}

func TestZeroStateCannotTransition(t *testing.T) {
	t.Parallel()

	if _, err := (State{}).Transition(PhaseAvailable); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Transition() error = %v, want %v", err, ErrInvalidState)
	}
}

func containsPhase(phases []Phase, target Phase) bool {
	return slices.Contains(phases, target)
}
