package nodelifecycleengine

import (
	"fmt"
	"slices"
)

// PlanInputはNode Lifecycle Plan生成に必要な不変入力である。
type PlanInput struct {
	OperationID string

	LifecycleRuntime LifecycleRuntime
	CurrentVersion   string
	TargetVersion    string
	UpdateClass      UpdateClass
	NodeRole         NodeRole
	SnapshotRef      string

	ControlPlaneAcceptedVersion     string
	RequireControlPlaneTargetAccept bool
}

// PlanはNode Lifecycle Serviceが型付きStepとして実行する順序を表す。
type Plan struct {
	OperationID      string
	LifecycleRuntime LifecycleRuntime
	CurrentVersion   string
	TargetVersion    string
	UpdateClass      UpdateClass
	NodeRole         NodeRole
	SnapshotRef      string
	Steps            []Step
}

// BuildPlanは対象Node種別に応じたNode Lifecycle Engine Step順序を作る。
func BuildPlan(input PlanInput) (Plan, error) {
	if input.OperationID == "" {
		return Plan{}, fmt.Errorf("operation ID is required")
	}
	result := DecidePlan(input)
	switch decision := result.(type) {
	case PlanReady:
		return decision.Plan, nil
	case PlanRejected:
		return Plan{}, fmt.Errorf("node lifecycle engine plan rejected: %T", decision.Failure)
	default:
		return Plan{}, fmt.Errorf("unsupported node lifecycle engine plan result %T", result)
	}
}

// ReadyForStepはStep実行直前に満たすべきPlan内の依存を検証する。
func ReadyForStep(plan Plan, step Step) error {
	switch decision := DecideStep(StepCommand{Plan: plan, Step: step}).(type) {
	case StepRunnable:
		return nil
	case StepBlocked:
		return fmt.Errorf("node lifecycle engine step blocked: %T", decision.Failure)
	default:
		return fmt.Errorf("unsupported node lifecycle engine step decision %T", decision)
	}
}

// RecordPlanStepはPlanが許可するStep順序に従い、完了済みStepを1回だけ追加する。
func RecordPlanStep(completed []Step, step Step, planSteps []Step) ([]Step, RecordDecision, error) {
	stepIndex := indexOfStep(planSteps, step)
	if stepIndex < 0 {
		return nil, RecordDecision{}, fmt.Errorf("lifecycle step %q is not part of this plan", step)
	}
	if slices.Contains(completed, step) {
		return append([]Step(nil), completed...), RecordDecision{AlreadyCompleted: true}, nil
	}
	if stepIndex != len(completed) {
		return nil, RecordDecision{}, fmt.Errorf("lifecycle step %q cannot be recorded before %q", step, planSteps[len(completed)])
	}
	next := append([]Step(nil), completed...)
	next = append(next, step)
	return next, RecordDecision{}, nil
}

func indexOfStep(steps []Step, step Step) int {
	for index, candidate := range steps {
		if candidate == step {
			return index
		}
	}
	return -1
}

func (input PlanInput) preflightInput() PreflightInput {
	return PreflightInput{
		LifecycleRuntime:                input.LifecycleRuntime,
		CurrentVersion:                  input.CurrentVersion,
		TargetVersion:                   input.TargetVersion,
		UpdateClass:                     input.UpdateClass,
		NodeRole:                        input.NodeRole,
		ControlPlaneAcceptedVersion:     input.ControlPlaneAcceptedVersion,
		RequireControlPlaneTargetAccept: input.RequireControlPlaneTargetAccept,
		SnapshotRef:                     input.SnapshotRef,
	}
}
