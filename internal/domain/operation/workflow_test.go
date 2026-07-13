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
	"testing"
	"time"
)

func TestDecideNextStepはPhaseとOperationKindから次Stepを選ぶ(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input WorkflowInput
		want  Step
	}{
		{
			name: "未開始OperationはPending初期化",
			input: WorkflowInput{
				Kind: KindProvision,
				Now:  now,
			},
			want: StepInitializePending{},
		},
		{
			name: "Pendingはboot準備",
			input: WorkflowInput{
				Kind:  KindProvision,
				Phase: PhasePending,
				Now:   now,
			},
			want: StepPrepareBoot{},
		},
		{
			name: "active phaseはdriver再観測",
			input: WorkflowInput{
				Kind:  KindUpdate,
				Phase: PhaseWriting,
				Now:   now,
			},
			want: StepObserveActive{},
		},
		{
			name: "WipeAllのAwaitingHealthはhost解放",
			input: WorkflowInput{
				Kind:  KindWipeAll,
				Phase: PhaseAwaitingHealth,
				Now:   now,
			},
			want: StepCompleteWipeAll{},
		},
		{
			name: "ProvisionのAwaitingHealthはMachine側のhealth待ち",
			input: WorkflowInput{
				Kind:  KindProvision,
				Phase: PhaseAwaitingHealth,
				Now:   now,
			},
			want: StepAwaitMachineHealth{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecideNextStep(tt.input)
			if err != nil {
				t.Fatalf("DecideNextStep() error = %v", err)
			}
			if got.Step != tt.want {
				t.Fatalf("DecideNextStep().Step = %#v, want %#v", got.Step, tt.want)
			}
			if len(got.Events) != 1 {
				t.Fatalf("DecideNextStep().Events length = %d, want 1", len(got.Events))
			}
		})
	}
}

func TestDecideNextStepはDeadline超過を優先する(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	got, err := DecideNextStep(WorkflowInput{
		Kind:     KindUpdate,
		Phase:    PhaseWriting,
		Deadline: now.Add(-time.Second),
		Now:      now,
	})
	if err != nil {
		t.Fatalf("DecideNextStep() error = %v", err)
	}
	if got.Step != (StepFailDeadlineExceeded{}) {
		t.Fatalf("DecideNextStep().Step = %#v, want StepFailDeadlineExceeded", got.Step)
	}
	if len(got.Events) != 1 {
		t.Fatalf("DecideNextStep().Events length = %d, want 1", len(got.Events))
	}
	if _, ok := got.Events[0].(EventDeadlineExceeded); !ok {
		t.Fatalf("DecideNextStep().Events[0] = %#v, want EventDeadlineExceeded", got.Events[0])
	}
}

func TestParseKindは未知のOperationKindを拒否する(t *testing.T) {
	t.Parallel()

	if _, err := ParseKind("Unknown"); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("ParseKind() error = %v, want ErrUnknownKind", err)
	}
}
