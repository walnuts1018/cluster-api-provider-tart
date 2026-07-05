package agentprogress

import "testing"

func TestEvaluateSequence(t *testing.T) {
	tests := []struct {
		name     string
		saved    int64
		incoming int64
		want     Decision
	}{
		{name: "first", saved: 0, incoming: 1, want: DecisionApply},
		{name: "next", saved: 1, incoming: 2, want: DecisionApply},
		{name: "duplicate", saved: 2, incoming: 2, want: DecisionDuplicate},
		{name: "older", saved: 2, incoming: 1, want: DecisionDuplicate},
		{name: "gap", saved: 2, incoming: 4, want: DecisionGap},
		{name: "fills gap", saved: 2, incoming: 3, want: DecisionApply},
		{name: "zero", saved: 0, incoming: 0, want: DecisionInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := EvaluateSequence(test.saved, test.incoming); got != test.want {
				t.Fatalf("EvaluateSequence(%d, %d) = %q, want %q", test.saved, test.incoming, got, test.want)
			}
		})
	}
}
