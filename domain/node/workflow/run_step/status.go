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
	"fmt"
	"slices"

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/node/entity/nodelifecycleengine"
)

// RecordCompletedStepはOperation StatusのcompletedSteps文字列をdomainの順序制約で更新する。
func RecordCompletedStep(
	completed []string,
	step domain.Step,
	plan domain.Plan,
) ([]string, domain.RecordDecision, error) {
	domainCompleted, err := parseCompletedSteps(completed, plan.Steps)
	if err != nil {
		return nil, domain.RecordDecision{}, err
	}
	next, decision, err := domain.RecordPlanStep(domainCompleted, step, plan.Steps)
	if err != nil {
		return nil, domain.RecordDecision{}, err
	}
	return formatCompletedSteps(next), decision, nil
}

func parseCompletedSteps(completed []string, planSteps []domain.Step) ([]domain.Step, error) {
	parsed := make([]domain.Step, 0, len(completed))
	seen := make(map[domain.Step]struct{}, len(completed))
	for index, value := range completed {
		step := domain.Step(value)
		if !stepInPlan(step, planSteps) {
			return nil, fmt.Errorf("completed lifecycle step %q is not part of this plan", value)
		}
		if _, ok := seen[step]; ok {
			return nil, fmt.Errorf("completed lifecycle step %q is duplicated", value)
		}
		seen[step] = struct{}{}
		if index >= len(planSteps) || planSteps[index] != step {
			return nil, fmt.Errorf("completed lifecycle step %q is out of order", value)
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
	return slices.Contains(planSteps, step)
}
