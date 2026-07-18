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

import (
	"testing"

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/node/entity/nodelifecycleengine"
)

func TestRecordCompletedStepはStatus用文字列をPlan順序で更新する(t *testing.T) {
	plan := workerPlan(t)

	completed, decision, err := RecordCompletedStep(nil, domain.StepPreflightCompleted, plan)
	if err != nil {
		t.Fatalf("RecordCompletedStep() error = %v", err)
	}
	if decision.AlreadyCompleted {
		t.Fatal("AlreadyCompleted = true, want false")
	}
	if len(completed) != 1 || completed[0] != string(domain.StepPreflightCompleted) {
		t.Fatalf("completed = %v, want PreflightCompleted", completed)
	}

	completed, decision, err = RecordCompletedStep(completed, domain.StepPreflightCompleted, plan)
	if err != nil {
		t.Fatalf("duplicate RecordCompletedStep() error = %v", err)
	}
	if !decision.AlreadyCompleted {
		t.Fatal("duplicate AlreadyCompleted = false, want true")
	}
	if len(completed) != 1 {
		t.Fatalf("duplicate completed = %v, want one item", completed)
	}
}

func TestRecordCompletedStepは不正なStatus文字列を拒否する(t *testing.T) {
	plan := workerPlan(t)

	_, _, err := RecordCompletedStep([]string{"UnknownStep"}, domain.StepTargetSlotWritten, plan)
	if err == nil {
		t.Fatal("RecordCompletedStep() error = nil, want invalid status error")
	}
}

func TestRecordCompletedStepは順序が壊れたStatusを拒否する(t *testing.T) {
	plan := workerPlan(t)

	_, _, err := RecordCompletedStep(
		[]string{string(domain.StepTargetSlotWritten)},
		domain.StepTargetSlotWritten,
		plan,
	)
	if err == nil {
		t.Fatal("RecordCompletedStep() error = nil, want invalid order error")
	}
}

func workerPlan(t *testing.T) domain.Plan {
	t.Helper()
	plan, err := domain.BuildPlan(domain.PlanInput{
		LifecycleRuntime: domain.LifecycleRuntimeKubeadm,
		OperationID:      "operation-1",
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         domain.NodeRoleWorker,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	return plan
}
