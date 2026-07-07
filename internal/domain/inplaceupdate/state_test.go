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

package inplaceupdate

import (
	"testing"

	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/slot"
)

func TestTransitionは更新失敗を旧SlotへのRollbackにする(t *testing.T) {
	tests := []struct {
		name  string
		phase operationdomain.Phase
		event Event
	}{
		{name: "write", phase: operationdomain.PhaseWriting, event: EventWriteFailed},
		{name: "verify", phase: operationdomain.PhaseVerifying, event: EventVerifyFailed},
		{name: "mount", phase: operationdomain.PhaseAwaitingHealth, event: EventMountFailed},
		{name: "Node health", phase: operationdomain.PhaseAwaitingHealth, event: EventNodeHealthFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := Transition(updateState(tt.phase, 0), tt.event)
			if err != nil {
				t.Fatalf("Transition() error = %v", err)
			}
			if decision.Phase != operationdomain.PhaseRollingBack {
				t.Fatalf("Phase = %q, want RollingBack", decision.Phase)
			}
			if decision.BootSlot != slotdomain.A {
				t.Fatalf("BootSlot = %q, want old slot A", decision.BootSlot)
			}
			if decision.HostPhase != hostdomain.PhaseUpdating {
				t.Fatalf("HostPhase = %q, want Updating", decision.HostPhase)
			}
		})
	}
}

func TestTransitionはBoot失敗3回で新Slotを選択しなくなる(t *testing.T) {
	state := updateState(operationdomain.PhaseBootTrial, 0)
	for attempt := int32(1); attempt <= 3; attempt++ {
		decision, err := Transition(state, EventBootFailed)
		if err != nil {
			t.Fatalf("attempt %d Transition() error = %v", attempt, err)
		}
		if decision.Attempt != attempt {
			t.Fatalf("attempt = %d, want %d", decision.Attempt, attempt)
		}
		if attempt < 3 {
			if decision.Phase != operationdomain.PhaseBootTrial || decision.BootSlot != slotdomain.B {
				t.Fatalf("attempt %d decision = %#v, want target slot retry", attempt, decision)
			}
			state.Attempt = decision.Attempt
			continue
		}
		if decision.Phase != operationdomain.PhaseRollingBack || decision.BootSlot != slotdomain.A {
			t.Fatalf("attempt 3 decision = %#v, want rollback to slot A", decision)
		}
		state.Phase = decision.Phase
		state.Attempt = decision.Attempt
	}

	decision, err := Transition(state, EventBootFailed)
	if err == nil {
		t.Fatalf("fourth boot failure decision = %#v, want invalid event", decision)
	}
	if decision.BootSlot == slotdomain.B {
		t.Fatal("fourth boot failure selected target slot B")
	}
}

func TestTransitionはRollback成功後に更新失敗を保持してReadyへ戻す(t *testing.T) {
	decision, err := Transition(updateState(operationdomain.PhaseRollingBack, 3), EventRollbackHealthy)
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if decision.Phase != operationdomain.PhaseFailed ||
		decision.HostPhase != hostdomain.PhaseProvisioned ||
		!decision.MachineReady ||
		!decision.FailureRetained ||
		decision.ActiveSlot != slotdomain.A {
		t.Fatalf("decision = %#v, want failed Operation on healthy old slot", decision)
	}
}

func TestTransitionは旧Slotも不健全ならRecoveryRequiredにする(t *testing.T) {
	decision, err := Transition(updateState(operationdomain.PhaseRollingBack, 3), EventRollbackUnhealthy)
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if decision.Phase != operationdomain.PhaseRecoveryRequired ||
		decision.HostPhase != hostdomain.PhaseRecoveryRequired ||
		decision.MachineReady ||
		!decision.FailureRetained {
		t.Fatalf("decision = %#v, want RecoveryRequired", decision)
	}
}

func TestTransitionはTarget健全時に新SlotをCommitする(t *testing.T) {
	decision, err := Transition(updateState(operationdomain.PhaseAwaitingHealth, 1), EventTargetHealthy)
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if decision.Phase != operationdomain.PhaseSucceeded ||
		decision.HostPhase != hostdomain.PhaseProvisioned ||
		!decision.MachineReady ||
		decision.FailureRetained ||
		decision.ActiveSlot != slotdomain.B {
		t.Fatalf("decision = %#v, want successful commit to slot B", decision)
	}
}

func updateState(phase operationdomain.Phase, attempt int32) State {
	return State{
		Phase:      phase,
		ActiveSlot: slotdomain.A,
		TargetSlot: slotdomain.B,
		Attempt:    attempt,
	}
}
