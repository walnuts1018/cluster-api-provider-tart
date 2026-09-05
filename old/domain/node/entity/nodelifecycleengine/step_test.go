package nodelifecycleengine

import "testing"

func TestRecordStepは完了Stepを順序通りに1回だけ追加する(t *testing.T) {
	completed, decision, err := RecordStep(nil, StepPreflightCompleted)
	if err != nil {
		t.Fatalf("RecordStep() error = %v", err)
	}
	if decision.AlreadyCompleted {
		t.Fatal("AlreadyCompleted = true, want false")
	}
	if len(completed) != 1 || completed[0] != StepPreflightCompleted {
		t.Fatalf("completed = %v, want PreflightCompleted", completed)
	}

	completed, decision, err = RecordStep(completed, StepPreflightCompleted)
	if err != nil {
		t.Fatalf("duplicate RecordStep() error = %v", err)
	}
	if !decision.AlreadyCompleted {
		t.Fatal("duplicate AlreadyCompleted = false, want true")
	}
	if len(completed) != 1 {
		t.Fatalf("duplicate completed = %v, want one item", completed)
	}
}

func TestRecordStepは順序飛ばしを拒否する(t *testing.T) {
	completed, decision, err := RecordStep(nil, StepDistributionApplied)
	if err == nil {
		t.Fatalf("RecordStep() completed=%v decision=%#v, want order error", completed, decision)
	}
}

func TestRecordStepは7Stepを最後まで記録する(t *testing.T) {
	var completed []Step
	for _, step := range LifecycleSteps() {
		next, decision, err := RecordStep(completed, step)
		if err != nil {
			t.Fatalf("RecordStep(%q) error = %v", step, err)
		}
		if decision.AlreadyCompleted {
			t.Fatalf("RecordStep(%q) AlreadyCompleted = true, want false", step)
		}
		completed = next
	}
	if len(completed) != 7 {
		t.Fatalf("completed = %v, want 7 steps", completed)
	}
	if !AllStepsCompleted(completed) {
		t.Fatalf("AllStepsCompleted(%v) = false, want true", completed)
	}
}
