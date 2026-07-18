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
	"reflect"
	"testing"
	"time"
)

func TestDecideはPhaseとOperationKindから次Resultを選ぶ(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input Command
		want  Result
	}{
		{
			name: "未開始OperationはPending初期化",
			input: Command{
				Kind: KindProvision,
				Now:  now,
			},
			want: InitializePending{
				Target: PhasePending,
				Events: []Event{EventOperationObserved{
					Kind: KindProvision,
				}},
			},
		},
		{
			name: "Pendingはboot準備",
			input: Command{
				Kind:  KindProvision,
				Phase: PhasePending,
				Now:   now,
			},
			want: PrepareBoot{
				Host: HostMarkProvisioning{},
				Events: []Event{EventOperationObserved{
					Kind:  KindProvision,
					Phase: PhasePending,
				}},
			},
		},
		{
			name: "PreparingBootは電源投入",
			input: Command{
				Kind:  KindProvision,
				Phase: PhasePreparingBoot,
				Now:   now,
			},
			want: ActivateBoot{
				Events: []Event{EventOperationObserved{
					Kind:  KindProvision,
					Phase: PhasePreparingBoot,
				}},
			},
		},
		{
			name: "active phaseはdriver再観測",
			input: Command{
				Kind:  KindUpdate,
				Phase: PhaseWriting,
				Now:   now,
			},
			want: ObserveActive{
				Events: []Event{EventOperationObserved{
					Kind:  KindUpdate,
					Phase: PhaseWriting,
				}},
			},
		},
		{
			name: "WipeAllのAwaitingHealthはhost解放",
			input: Command{
				Kind:  KindWipeAll,
				Phase: PhaseAwaitingHealth,
				Now:   now,
			},
			want: CompleteOperation{
				Host:   HostMarkAvailable{},
				Target: PhaseSucceeded,
				Events: []Event{EventOperationObserved{
					Kind:  KindWipeAll,
					Phase: PhaseAwaitingHealth,
				}},
			},
		},
		{
			name: "ProvisionのAwaitingHealthはMachine側のhealth待ち",
			input: Command{
				Kind:  KindProvision,
				Phase: PhaseAwaitingHealth,
				Now:   now,
			},
			want: AwaitMachineHealth{
				Events: []Event{EventOperationObserved{
					Kind:  KindProvision,
					Phase: PhaseAwaitingHealth,
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Decide(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decide() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecideはDeadline超過を優先する(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	got := Decide(Command{
		Kind:     KindUpdate,
		Phase:    PhaseWriting,
		Deadline: now.Add(-time.Second),
		Now:      now,
	})
	want := DeadlineExceeded{
		Outcome: DeadlineMarkFailed{
			WithUpdateFailure: true,
			FailedPhase:       PhaseWriting,
		},
		Events: []Event{EventDeadlineExceeded{
			Kind:     KindUpdate,
			Phase:    PhaseWriting,
			Deadline: now.Add(-time.Second),
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Decide() = %#v, want %#v", got, want)
	}
}

func TestDecideはOperation種別とPolicyからHostCommandを決める(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		input  Command
		expect Result
	}{
		{
			name: "Update開始はUpdatingへ移す",
			input: Command{
				Kind:  KindUpdate,
				Phase: PhasePending,
				Now:   now,
			},
			expect: PrepareBoot{
				Host: HostMarkUpdating{},
				Events: []Event{EventOperationObserved{
					Kind:  KindUpdate,
					Phase: PhasePending,
				}},
			},
		},
		{
			name: "Clean開始はDeletionPolicy付きCleaningへ移す",
			input: Command{
				Kind:           KindClean,
				Phase:          PhasePending,
				CleaningPolicy: CleaningPolicyRetainData,
				Now:            now,
			},
			expect: PrepareBoot{
				Host: HostMarkCleaning{Policy: CleaningPolicyRetainData},
				Events: []Event{EventOperationObserved{
					Kind:  KindClean,
					Phase: PhasePending,
				}},
			},
		},
		{
			name: "RetainStateのClean完了はDetachedへ移す",
			input: Command{
				Kind:           KindClean,
				Phase:          PhaseAwaitingHealth,
				CleaningPolicy: CleaningPolicyRetainState,
				Now:            now,
			},
			expect: CompleteOperation{
				Host:   HostMarkDetached{},
				Target: PhaseSucceeded,
				Events: []Event{EventOperationObserved{
					Kind:  KindClean,
					Phase: PhaseAwaitingHealth,
				}},
			},
		},
		{
			name: "UpdateのRecoveryRequiredはHostもRecoveryRequiredへ移す",
			input: Command{
				Kind:  KindUpdate,
				Phase: PhaseRecoveryRequired,
				Now:   now,
			},
			expect: HandleTerminal{
				Host: HostMarkRecoveryRequired{},
				Events: []Event{EventTerminalObserved{
					Kind:  KindUpdate,
					Phase: PhaseRecoveryRequired,
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Decide(tt.input)
			if !reflect.DeepEqual(got, tt.expect) {
				t.Fatalf("Decide() = %#v, want %#v", got, tt.expect)
			}
		})
	}
}

func TestDecideはCleanのPolicy未解決を型付き失敗として返す(t *testing.T) {
	t.Parallel()

	got := Decide(Command{
		Kind:  KindClean,
		Phase: PhaseAwaitingHealth,
		Now:   time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
	})
	rejected, ok := got.(Rejected)
	if !ok {
		t.Fatalf("Decide() = %#v, want Rejected", got)
	}
	if _, ok := rejected.Failure.(CleaningPolicyRequired); !ok {
		t.Fatalf("Rejected.Failure = %#v, want CleaningPolicyRequired", rejected.Failure)
	}
}

func TestParseKindは未知のOperationKindを拒否する(t *testing.T) {
	t.Parallel()

	if _, err := ParseKind("Unknown"); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("ParseKind() error = %v, want ErrUnknownKind", err)
	}
}
