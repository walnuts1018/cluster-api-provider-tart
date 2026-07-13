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

func TestProcessはPhaseとOperationKindから次Commandを選ぶ(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input ProcessState
		want  Command
	}{
		{
			name: "未開始OperationはPending初期化",
			input: ProcessState{
				Kind: KindProvision,
			},
			want: CommandInitializePending{Target: PhasePending},
		},
		{
			name: "Pendingはboot準備",
			input: ProcessState{
				Kind:  KindProvision,
				Phase: PhasePending,
			},
			want: CommandPrepareBoot{Host: HostMarkProvisioning{}},
		},
		{
			name: "active phaseはdriver再観測",
			input: ProcessState{
				Kind:  KindUpdate,
				Phase: PhaseWriting,
			},
			want: CommandObserveActive{},
		},
		{
			name: "WipeAllのAwaitingHealthはhost解放",
			input: ProcessState{
				Kind:  KindWipeAll,
				Phase: PhaseAwaitingHealth,
			},
			want: CommandCompleteWipeAll{Host: HostMarkAvailable{}, Target: PhaseSucceeded},
		},
		{
			name: "ProvisionのAwaitingHealthはMachine側のhealth待ち",
			input: ProcessState{
				Kind:  KindProvision,
				Phase: PhaseAwaitingHealth,
			},
			want: CommandAwaitMachineHealth{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Process(ProcessInput{State: tt.input, Now: now})
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			if got.Command != tt.want {
				t.Fatalf("Process().Command = %#v, want %#v", got.Command, tt.want)
			}
			if len(got.Events) != 1 {
				t.Fatalf("Process().Events length = %d, want 1", len(got.Events))
			}
		})
	}
}

func TestProcessはDeadline超過を優先する(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	got, err := Process(ProcessInput{
		State: ProcessState{
			Kind:     KindUpdate,
			Phase:    PhaseWriting,
			Deadline: now.Add(-time.Second),
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	want := CommandFailDeadlineExceeded{Outcome: DeadlineMarkFailed{
		WithUpdateFailure: true,
		FailedPhase:       PhaseWriting,
	}}
	if got.Command != want {
		t.Fatalf("Process().Command = %#v, want %#v", got.Command, want)
	}
	if len(got.Events) != 1 {
		t.Fatalf("Process().Events length = %d, want 1", len(got.Events))
	}
	if _, ok := got.Events[0].(EventDeadlineExceeded); !ok {
		t.Fatalf("Process().Events[0] = %#v, want EventDeadlineExceeded", got.Events[0])
	}
}

func TestProcessはOperation種別とPolicyからHostCommandを決める(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		state  ProcessState
		expect Command
	}{
		{
			name: "Update開始はUpdatingへ移す",
			state: ProcessState{
				Kind:  KindUpdate,
				Phase: PhasePending,
			},
			expect: CommandPrepareBoot{Host: HostMarkUpdating{}},
		},
		{
			name: "Clean開始はDeletionPolicy付きCleaningへ移す",
			state: ProcessState{
				Kind:           KindClean,
				Phase:          PhasePending,
				CleaningPolicy: CleaningPolicyRetainData,
			},
			expect: CommandPrepareBoot{Host: HostMarkCleaning{Policy: CleaningPolicyRetainData}},
		},
		{
			name: "RetainStateのClean完了はDetachedへ移す",
			state: ProcessState{
				Kind:           KindClean,
				Phase:          PhaseAwaitingHealth,
				CleaningPolicy: CleaningPolicyRetainState,
			},
			expect: CommandCompleteCleaning{Host: HostMarkDetached{}, Target: PhaseSucceeded},
		},
		{
			name: "UpdateのRecoveryRequiredはHostもRecoveryRequiredへ移す",
			state: ProcessState{
				Kind:  KindUpdate,
				Phase: PhaseRecoveryRequired,
			},
			expect: CommandHandleTerminal{Host: HostMarkRecoveryRequired{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Process(ProcessInput{State: tt.state, Now: now})
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			if got.Command != tt.expect {
				t.Fatalf("Process().Command = %#v, want %#v", got.Command, tt.expect)
			}
		})
	}
}

func TestProcessはCleanのPolicy未解決を拒否する(t *testing.T) {
	t.Parallel()

	_, err := Process(ProcessInput{
		State: ProcessState{
			Kind:  KindClean,
			Phase: PhaseAwaitingHealth,
		},
		Now: time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrUnknownCleaningPolicy) {
		t.Fatalf("Process() error = %v, want ErrUnknownCleaningPolicy", err)
	}
}

func TestParseKindは未知のOperationKindを拒否する(t *testing.T) {
	t.Parallel()

	if _, err := ParseKind("Unknown"); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("ParseKind() error = %v, want ErrUnknownKind", err)
	}
}
