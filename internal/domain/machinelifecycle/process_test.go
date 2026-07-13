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

package machinelifecycle

import (
	"testing"

	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func TestDecideOperationはMachineとOperationの状態からCommandを選ぶ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		machine   MachineState
		operation OperationState
		want      OperationCommand
	}{
		{
			name:      "未Provisionedの進行中OperationはProvision healthを観察",
			machine:   MachineState{Provisioned: false, HasOperation: true},
			operation: OperationState{Kind: operationdomain.KindProvision, Phase: operationdomain.PhaseBootTrial},
			want:      CommandObserveProvisionHealth{},
		},
		{
			name:      "未Provisionedの失敗OperationはProvision失敗へ反映",
			machine:   MachineState{Provisioned: false, HasOperation: true},
			operation: OperationState{Kind: operationdomain.KindProvision, Phase: operationdomain.PhaseFailed},
			want: CommandMarkProvisionFailed{
				Reason:  "OperationFailed",
				Message: "TartHostOperation finished in Failed",
			},
		},
		{
			name:      "ProvisionedのUpdate AwaitingHealthはUpdate healthを観察",
			machine:   MachineState{Provisioned: true, HasOperation: true},
			operation: OperationState{Kind: operationdomain.KindUpdate, Phase: operationdomain.PhaseAwaitingHealth},
			want:      CommandObserveUpdateHealth{},
		},
		{
			name:      "ProvisionedのUpdate SucceededはUpdate成功を反映",
			machine:   MachineState{Provisioned: true, HasOperation: true},
			operation: OperationState{Kind: operationdomain.KindUpdate, Phase: operationdomain.PhaseSucceeded},
			want:      CommandApplyUpdateTerminal{Outcome: UpdateOutcomeSucceeded},
		},
		{
			name:      "Provisionedの非Update Operationは通常のNodeHealth観察へ戻す",
			machine:   MachineState{Provisioned: true, HasOperation: true},
			operation: OperationState{Kind: operationdomain.KindClean, Phase: operationdomain.PhaseAwaitingHealth},
			want:      CommandObserveNodeHealth{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecideOperation(tt.machine, tt.operation)
			if err != nil {
				t.Fatalf("DecideOperation() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("DecideOperation() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecideLifecycleはTartMachine全体の観測状態をCommandへ写像する(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		observed ObservedState
		want     LifecycleCommand
	}{
		{
			name:     "削除中でないMachineはActive reconcileへ進む",
			observed: ObservedActive{},
			want:     CommandReconcileActive{},
		},
		{
			name:     "削除中でFinalizerがあるMachineは削除Workflowへ進む",
			observed: ObservedDeleting{FinalizerPresent: true},
			want:     CommandFinalizeDeleting{},
		},
		{
			name:     "削除中でFinalizerがないMachineは何もしない",
			observed: ObservedDeleting{FinalizerPresent: false},
			want:     CommandIgnoreDeleting{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecideLifecycle(tt.observed)
			if err != nil {
				t.Fatalf("DecideLifecycle() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("DecideLifecycle() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecideProvisionはOperationRefの有無から開始と再開を選ぶ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state MachineState
		want  ProvisionCommand
	}{
		{
			name:  "OperationRefなしならProvisionを開始",
			state: MachineState{Provisioned: false, HasOperation: false},
			want:  CommandStartProvision{},
		},
		{
			name:  "OperationRefありならProvision Operationを再開",
			state: MachineState{Provisioned: false, HasOperation: true},
			want:  CommandResumeProvisionOperation{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := DecideProvision(tt.state); got != tt.want {
				t.Fatalf("DecideProvision() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecideProvisionHealthはReadiness結果をProvisionCommandへ写像する(t *testing.T) {
	t.Parallel()

	if got := DecideProvisionHealth(Readiness{Ready: true}); got != (CommandCompleteProvision{}) {
		t.Fatalf("DecideProvisionHealth(ready) = %#v, want complete", got)
	}

	got := DecideProvisionHealth(Readiness{Reason: "WaitingForBoot", Message: "waiting"})
	want := CommandSetProvisionHealthPending{Reason: "WaitingForBoot", Message: "waiting"}
	if got != want {
		t.Fatalf("DecideProvisionHealth(pending) = %#v, want %#v", got, want)
	}
}

func TestDecideUpdateHealthはNodeHealth結果をUpdateCommandへ写像する(t *testing.T) {
	t.Parallel()

	if got := DecideUpdateHealth(machinehealthdomain.Result{Ready: true}); got != (CommandCompleteUpdate{}) {
		t.Fatalf("DecideUpdateHealth(ready) = %#v, want complete", got)
	}
	if got := DecideUpdateHealth(machinehealthdomain.Result{Ready: false}); got != (CommandRollbackUpdate{}) {
		t.Fatalf("DecideUpdateHealth(unhealthy) = %#v, want rollback", got)
	}
}
