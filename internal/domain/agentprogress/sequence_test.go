package agentprogress

import "testing"

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		saved    State
		sequence int64
		incoming Progress
		want     Decision
	}{
		{name: "first", sequence: 1, incoming: Progress{Step: "WriteImage", Percent: 10}, want: DecisionApply},
		{name: "next", saved: State{Sequence: 1}, sequence: 2, incoming: Progress{Step: "WriteImage", Percent: 10}, want: DecisionApply},
		{name: "duplicate", saved: State{Sequence: 2}, sequence: 2, incoming: Progress{}, want: DecisionDuplicate},
		{name: "older", saved: State{Sequence: 2}, sequence: 1, incoming: Progress{}, want: DecisionDuplicate},
		{name: "gap", saved: State{Sequence: 2}, sequence: 4, incoming: Progress{}, want: DecisionGap},
		{name: "zero", sequence: 0, incoming: Progress{}, want: DecisionInvalid},
		{name: "empty step", sequence: 1, incoming: Progress{Percent: 10}, want: DecisionInvalid},
		{name: "invalid increment", sequence: 1, incoming: Progress{Step: "WriteImage", Percent: 15}, want: DecisionInvalid},
		{name: "completed below 100", sequence: 1, incoming: Progress{Step: "WriteImage", Percent: 90, Completed: true}, want: DecisionInvalid},
		{
			name: "progress regression",
			saved: State{
				Sequence: 1,
				Progress: &Progress{Step: "WriteImage", DiskRole: "OS-A", Percent: 50},
			},
			sequence: 2,
			incoming: Progress{Step: "WriteImage", DiskRole: "OS-A", Percent: 40},
			want:     DecisionInvalid,
		},
		{
			name: "next disk may restart percentage",
			saved: State{
				Sequence: 1,
				Progress: &Progress{Step: "WriteImage", DiskRole: "OS-A", Percent: 100},
			},
			sequence: 2,
			incoming: Progress{Step: "WriteImage", DiskRole: "Verity-A", Percent: 10},
			want:     DecisionApply,
		},
		{
			name: "completed step cannot regress",
			saved: State{
				Sequence:       1,
				CompletedSteps: []string{"WriteImage"},
			},
			sequence: 2,
			incoming: Progress{Step: "WriteImage", DiskRole: "OS-A", Percent: 10},
			want:     DecisionInvalid,
		},
		{
			name:     "verify requires completed write",
			sequence: 1,
			incoming: Progress{Step: "VerifyImage", Percent: 100, Completed: true},
			want:     DecisionInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Evaluate(test.saved, test.sequence, test.incoming); got.Decision != test.want {
				t.Fatalf("Evaluate() decision = %q, want %q", got.Decision, test.want)
			}
		})
	}
}

func TestEvaluateCompletesStepWithoutMutatingInput(t *testing.T) {
	saved := State{CompletedSteps: []string{"Inventory"}}
	result := Evaluate(saved, 1, Progress{Step: "WriteImage", Percent: 100, Completed: true})

	if result.Decision != DecisionApply {
		t.Fatalf("Evaluate() decision = %q, want %q", result.Decision, DecisionApply)
	}
	if len(result.State.CompletedSteps) != 2 || result.State.CompletedSteps[1] != "WriteImage" {
		t.Fatalf("CompletedSteps = %#v", result.State.CompletedSteps)
	}
	if len(saved.CompletedSteps) != 1 {
		t.Fatalf("input state was mutated: %#v", saved.CompletedSteps)
	}
}

func TestNextPhase(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		progress Progress
		want     string
	}{
		{name: "write starts", current: "WaitingForAgent", progress: Progress{Step: "WriteImage"}, want: "Writing"},
		{
			name:     "write completion starts verification",
			current:  "Writing",
			progress: Progress{Step: "WriteImage", Completed: true},
			want:     "Verifying",
		},
		{name: "verify remains verifying", current: "Verifying", progress: Progress{Step: "VerifyImage"}, want: "Verifying"},
		{name: "late progress cannot regress boot trial", current: "BootTrial", progress: Progress{Step: "WriteImage"}, want: "BootTrial"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NextPhase(test.current, test.progress); got != test.want {
				t.Fatalf("NextPhase() = %q, want %q", got, test.want)
			}
		})
	}
}
