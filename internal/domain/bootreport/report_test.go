package bootreport

import (
	"errors"
	"testing"

	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func TestEvaluate(t *testing.T) {
	complete := Report{
		BootID:             "boot-id",
		ActiveSlot:         "B",
		ArtifactGeneration: 2,
		StateMounted:       true,
		DataMounted:        true,
		BootstrapApplied:   true,
	}
	expected := ExpectedBoot{ActiveSlot: "B", ArtifactGeneration: 2}

	tests := []struct {
		name         string
		phase        operationdomain.Phase
		current      *Report
		incoming     Report
		wantDecision Decision
		wantPhase    operationdomain.Phase
		wantErr      error
	}{
		{
			name:         "complete report advances the operation",
			phase:        operationdomain.PhaseBootTrial,
			incoming:     complete,
			wantDecision: DecisionCompleted,
			wantPhase:    operationdomain.PhaseAwaitingHealth,
		},
		{
			name:         "failed mount remains observable without advancing",
			phase:        operationdomain.PhaseBootTrial,
			incoming:     Report{BootID: "boot-id", ActiveSlot: "B", ArtifactGeneration: 2},
			wantDecision: DecisionRecorded,
			wantPhase:    operationdomain.PhaseBootTrial,
		},
		{
			name:         "unexpected slot remains observable without advancing",
			phase:        operationdomain.PhaseBootTrial,
			incoming:     Report{BootID: "boot-id", ActiveSlot: "A", ArtifactGeneration: 2, StateMounted: true, DataMounted: true, BootstrapApplied: true},
			wantDecision: DecisionRecorded,
			wantPhase:    operationdomain.PhaseBootTrial,
		},
		{
			name:         "same report is idempotent",
			phase:        operationdomain.PhaseAwaitingHealth,
			current:      &complete,
			incoming:     complete,
			wantDecision: DecisionDuplicate,
			wantPhase:    operationdomain.PhaseAwaitingHealth,
		},
		{
			name:     "completed trial rejects a different report",
			phase:    operationdomain.PhaseAwaitingHealth,
			current:  &complete,
			incoming: Report{BootID: "another-boot"},
			wantErr:  ErrConflictingCompleted,
		},
		{
			name:     "report before boot trial is rejected",
			phase:    operationdomain.PhaseVerifying,
			incoming: complete,
			wantErr:  ErrUnexpectedPhase,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Evaluate(test.phase, test.current, test.incoming, expected)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Evaluate() error = %v, want %v", err, test.wantErr)
			}
			if got.Decision != test.wantDecision {
				t.Fatalf("Evaluate() decision = %q, want %q", got.Decision, test.wantDecision)
			}
			if got.NextPhase != test.wantPhase {
				t.Fatalf("Evaluate() next phase = %q, want %q", got.NextPhase, test.wantPhase)
			}
		})
	}
}
