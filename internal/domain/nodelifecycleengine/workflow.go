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

package nodelifecycleengine

func DecidePlan(input PlanInput) PlanResult {
	if failure := preflightFailure(input.preflightInput()); failure != nil {
		return PlanRejected{Failure: failure}
	}

	steps := []Step{StepPreflightCompleted}
	if input.NodeRole == NodeRoleControlPlane || input.UpdateClass == UpdateClassStateMigration {
		steps = append(steps, StepSnapshotCreated)
	}
	steps = append(steps,
		StepTargetSlotWritten,
		StepDistributionApplied,
		StepTargetSlotBooted,
		StepHealthVerified,
		StepCommitted,
	)

	return PlanReady{Plan: Plan{
		OperationID:      input.OperationID,
		LifecycleRuntime: input.LifecycleRuntime,
		CurrentVersion:   input.CurrentVersion,
		TargetVersion:    input.TargetVersion,
		UpdateClass:      input.UpdateClass,
		NodeRole:         input.NodeRole,
		SnapshotRef:      input.SnapshotRef,
		Steps:            steps,
	}}
}

func DecideStep(command StepCommand) RunnableDecision {
	if indexOfStep(command.Plan.Steps, command.Step) < 0 {
		return StepBlocked{Failure: StepNotInPlan{Step: command.Step}}
	}
	if command.Step == StepDistributionApplied &&
		(command.Plan.NodeRole == NodeRoleControlPlane ||
			command.Plan.UpdateClass == UpdateClassStateMigration) &&
		command.Plan.SnapshotRef == "" {
		return StepBlocked{Failure: SnapshotRequired{Step: command.Step}}
	}
	return StepRunnable{}
}

func DecideHealth(input HealthInput) HealthDecision {
	failures := make([]HealthGateFailure, 0)
	if !input.NodeReady {
		failures = append(failures, NodeNotReady{})
	}
	if input.NodeVersion != input.TargetVersion {
		failures = append(failures, VersionMismatch{
			Current: input.NodeVersion,
			Target:  input.TargetVersion,
		})
	}
	if input.NodeRole == NodeRoleControlPlane {
		if !input.StaticPodsReady {
			failures = append(failures, StaticPodsNotReady{})
		}
		if !input.EtcdQuorum {
			failures = append(failures, EtcdQuorumLost{})
		}
		if !input.APIHealthy {
			failures = append(failures, APIUnhealthy{})
		}
	}
	if len(failures) == 0 {
		return HealthGateSatisfied{}
	}
	return HealthGateBlocked{Failures: failures}
}
