package nodelifecycleengine

import "fmt"

// StepはNode Lifecycle Engine Operationで永続化する冪等stepである。
type Step string

const (
	StepPreflightCompleted  Step = "PreflightCompleted"
	StepSnapshotCreated     Step = "SnapshotCreated"
	StepTargetSlotWritten   Step = "TargetSlotWritten"
	StepDistributionApplied Step = "DistributionApplied"
	StepTargetSlotBooted    Step = "TargetSlotBooted"
	StepHealthVerified      Step = "HealthVerified"
	StepCommitted           Step = "Committed"
)

var lifecycleSteps = []Step{
	StepPreflightCompleted,
	StepSnapshotCreated,
	StepTargetSlotWritten,
	StepDistributionApplied,
	StepTargetSlotBooted,
	StepHealthVerified,
	StepCommitted,
}

// RecordDecisionはStep reportを永続化すべきかを表す。
type RecordDecision struct {
	AlreadyCompleted bool
}

// LifecycleStepsは共通Lifecycle Step順序のcopyを返す。
func LifecycleSteps() []Step {
	return append([]Step(nil), lifecycleSteps...)
}

// RecordStepは完了済みStep集合へ新しいStepを順序通りに追加する。
func RecordStep(completed []Step, step Step) ([]Step, RecordDecision, error) {
	if stepOrder(step) < 0 {
		return nil, RecordDecision{}, fmt.Errorf("unknown lifecycle step %q", step)
	}
	return RecordPlanStep(completed, step, lifecycleSteps)
}

// AllStepsCompletedは共通Lifecycle Stepがすべて順序通り完了済みかを返す。
func AllStepsCompleted(completed []Step) bool {
	if len(completed) != len(lifecycleSteps) {
		return false
	}
	for index, step := range completed {
		if lifecycleSteps[index] != step {
			return false
		}
	}
	return true
}

func stepOrder(step Step) int {
	for index, candidate := range lifecycleSteps {
		if candidate == step {
			return index
		}
	}
	return -1
}
