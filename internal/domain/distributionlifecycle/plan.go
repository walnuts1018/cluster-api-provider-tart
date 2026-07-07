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

package distributionlifecycle

import "fmt"

// PlanInputはDistribution Lifecycle Plan生成に必要な不変入力である。
type PlanInput struct {
	OperationID string

	CurrentVersion string
	TargetVersion  string
	UpdateClass    UpdateClass
	NodeRole       NodeRole
	SnapshotRef    string
}

// PlanはNode Lifecycle Serviceが型付きStepとして実行する順序を表す。
type Plan struct {
	OperationID    string
	CurrentVersion string
	TargetVersion  string
	UpdateClass    UpdateClass
	NodeRole       NodeRole
	SnapshotRef    string
	Steps          []Step
}

// BuildPlanは対象Node種別に応じたDistribution Lifecycle Step順序を作る。
func BuildPlan(input PlanInput) (Plan, error) {
	if input.OperationID == "" {
		return Plan{}, fmt.Errorf("Operation ID is required")
	}
	preflight := PreflightInput{
		CurrentVersion: input.CurrentVersion,
		TargetVersion:  input.TargetVersion,
		UpdateClass:    input.UpdateClass,
		NodeRole:       input.NodeRole,
		SnapshotRef:    input.SnapshotRef,
	}
	if err := Preflight(preflight); err != nil {
		return Plan{}, err
	}

	steps := []Step{
		StepPreflightCompleted,
	}
	if input.NodeRole == NodeRoleControlPlane || input.UpdateClass == UpdateClassStateMigration {
		steps = append(steps, StepSnapshotCreated)
	}
	steps = append(steps,
		StepTargetSlotWritten,
		StepKubeadmApplied,
		StepTargetSlotBooted,
		StepHealthVerified,
		StepCommitted,
	)

	return Plan{
		OperationID:    input.OperationID,
		CurrentVersion: input.CurrentVersion,
		TargetVersion:  input.TargetVersion,
		UpdateClass:    input.UpdateClass,
		NodeRole:       input.NodeRole,
		SnapshotRef:    input.SnapshotRef,
		Steps:          steps,
	}, nil
}

// ReadyForStepはStep実行直前に満たすべきPlan内の依存を検証する。
func ReadyForStep(plan Plan, step Step) error {
	if indexOfStep(plan.Steps, step) < 0 {
		return fmt.Errorf("lifecycle step %q is not part of this plan", step)
	}
	if step == StepKubeadmApplied &&
		(plan.NodeRole == NodeRoleControlPlane || plan.UpdateClass == UpdateClassStateMigration) &&
		plan.SnapshotRef == "" {
		return fmt.Errorf("SnapshotRef is required before kubeadm apply")
	}
	return nil
}

// RecordPlanStepはPlanが許可するStep順序に従い、完了済みStepを1回だけ追加する。
func RecordPlanStep(completed []Step, step Step, planSteps []Step) ([]Step, StepDecision, error) {
	stepIndex := indexOfStep(planSteps, step)
	if stepIndex < 0 {
		return nil, StepDecision{}, fmt.Errorf("lifecycle step %q is not part of this plan", step)
	}
	for _, existing := range completed {
		if existing == step {
			return append([]Step(nil), completed...), StepDecision{AlreadyCompleted: true}, nil
		}
	}
	if stepIndex != len(completed) {
		return nil, StepDecision{}, fmt.Errorf("lifecycle step %q cannot be recorded before %q", step, planSteps[len(completed)])
	}
	next := append([]Step(nil), completed...)
	next = append(next, step)
	return next, StepDecision{}, nil
}

func indexOfStep(steps []Step, step Step) int {
	for index, candidate := range steps {
		if candidate == step {
			return index
		}
	}
	return -1
}
