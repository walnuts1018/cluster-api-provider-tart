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

import (
	"fmt"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

// RecordCompletedStepはOperation StatusのcompletedSteps文字列をdomainの順序制約で更新する。
func RecordCompletedStep(
	completed []string,
	step domain.Step,
	plan domain.Plan,
) ([]string, domain.StepDecision, error) {
	domainCompleted, err := parseCompletedSteps(completed, plan.Steps)
	if err != nil {
		return nil, domain.StepDecision{}, err
	}
	next, decision, err := domain.RecordPlanStep(domainCompleted, step, plan.Steps)
	if err != nil {
		return nil, domain.StepDecision{}, err
	}
	return formatCompletedSteps(next), decision, nil
}

func parseCompletedSteps(completed []string, planSteps []domain.Step) ([]domain.Step, error) {
	parsed := make([]domain.Step, 0, len(completed))
	for _, value := range completed {
		step := domain.Step(value)
		if !stepInPlan(step, planSteps) {
			return nil, fmt.Errorf("completed lifecycle step %q is not part of this plan", value)
		}
		parsed = append(parsed, step)
	}
	return parsed, nil
}

func formatCompletedSteps(completed []domain.Step) []string {
	formatted := make([]string, 0, len(completed))
	for _, step := range completed {
		formatted = append(formatted, string(step))
	}
	return formatted
}

func stepInPlan(step domain.Step, planSteps []domain.Step) bool {
	for _, candidate := range planSteps {
		if candidate == step {
			return true
		}
	}
	return false
}
