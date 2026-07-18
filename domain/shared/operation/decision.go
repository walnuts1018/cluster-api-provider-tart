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

func Decide(command Command) Result {
	if failure := validateCleaningPolicy(command); failure != nil {
		return Rejected{Failure: failure}
	}
	if command.Phase.Terminal() {
		return HandleTerminal{
			Host:   terminalHostCommand(command),
			Events: []Event{EventTerminalObserved{Kind: command.Kind, Phase: command.Phase}},
		}
	}
	if !command.Deadline.IsZero() && command.Now.After(command.Deadline) {
		return DeadlineExceeded{
			Outcome: deadlineOutcome(command),
			Events: []Event{
				EventDeadlineExceeded{Kind: command.Kind, Phase: command.Phase, Deadline: command.Deadline},
			},
		}
	}

	observed := []Event{EventOperationObserved{Kind: command.Kind, Phase: command.Phase}}
	switch command.Phase {
	case "":
		return InitializePending{Target: PhasePending, Events: observed}
	case PhasePending:
		return PrepareBoot{Host: pendingHostCommand(command), Events: observed}
	case PhasePreparingBoot:
		return ActivateBoot{Events: observed}
	case PhaseWaitingForAgent,
		PhaseWriting,
		PhaseVerifying,
		PhaseBootTrial:
		return ObserveActive{Events: observed}
	case PhaseAwaitingHealth:
		switch command.Kind {
		case KindProvision, KindUpdate, KindRollback, KindRecovery:
			return AwaitMachineHealth{Events: observed}
		case KindWipeAll:
			return CompleteOperation{
				Host:   HostMarkAvailable{},
				Target: PhaseSucceeded,
				Events: observed,
			}
		case KindClean:
			return CompleteOperation{
				Host:   completedCleaningHostCommand(command),
				Target: PhaseSucceeded,
				Events: observed,
			}
		}
	case PhaseDistributionUpdating,
		PhaseRollingBack,
		PhaseSucceeded,
		PhaseFailed,
		PhaseRecoveryRequired:
		return Ignore{Events: observed}
	}
	return Ignore{Events: observed}
}

func validateCleaningPolicy(command Command) Failure {
	if command.Kind != KindClean {
		return nil
	}
	//nolint:exhaustive // Cleaning policyが必要な境界phaseだけを検査する。
	switch command.Phase {
	case PhasePending, PhaseAwaitingHealth:
		switch command.CleaningPolicy {
		case CleaningPolicyRetainData, CleaningPolicyRetainState:
			return nil
		case CleaningPolicyUnspecified, CleaningPolicyWipeAll:
			return CleaningPolicyRequired{Kind: command.Kind, Phase: command.Phase}
		}
	}
	return nil
}

func pendingHostCommand(command Command) HostCommand {
	switch command.Kind {
	case KindUpdate:
		return HostMarkUpdating{}
	case KindClean:
		return HostMarkCleaning{Policy: command.CleaningPolicy}
	case KindWipeAll:
		return HostMarkCleaning{Policy: CleaningPolicyWipeAll}
	case KindProvision, KindRollback, KindRecovery:
		return HostMarkProvisioning{}
	}
	return HostNoop{}
}

func completedCleaningHostCommand(command Command) HostCommand {
	switch command.CleaningPolicy {
	case CleaningPolicyRetainData:
		return HostMarkRetained{}
	case CleaningPolicyRetainState:
		return HostMarkDetached{}
	case CleaningPolicyUnspecified, CleaningPolicyWipeAll:
		return HostNoop{}
	}
	return HostNoop{}
}

func terminalHostCommand(command Command) HostCommand {
	if command.Kind != KindUpdate {
		return HostNoop{}
	}
	//nolint:exhaustive // terminal phase以外はHost更新なしとして扱う。
	switch command.Phase {
	case PhaseSucceeded, PhaseFailed:
		return HostMarkProvisioned{}
	case PhaseRecoveryRequired:
		return HostMarkRecoveryRequired{}
	}
	return HostNoop{}
}

func deadlineOutcome(command Command) DeadlineOutcome {
	if command.Kind != KindUpdate {
		return DeadlineMarkFailed{}
	}
	//nolint:exhaustive // deadline時に特別な復旧経路が必要なphaseだけを列挙する。
	switch command.Phase {
	case PhaseBootTrial:
		return DeadlineRecordBootFailure{}
	case PhaseAwaitingHealth:
		return DeadlineTransitionFailure{FailedPhase: PhaseAwaitingHealth, Target: PhaseRollingBack}
	case PhaseRollingBack:
		return DeadlineTransitionFailure{FailedPhase: PhaseRollingBack, Target: PhaseRecoveryRequired}
	default:
		return DeadlineMarkFailed{WithUpdateFailure: true, FailedPhase: command.Phase}
	}
}
