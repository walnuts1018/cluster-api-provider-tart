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

package step

import (
	"fmt"
	"slices"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

func EnsureRunnable(plan domain.Plan, step domain.Step) error {
	if !stepInPlan(step, plan.Steps) {
		return fmt.Errorf("lifecycle step %q is not part of this plan", step)
	}
	return domain.ReadyForStep(plan, step)
}

func stepInPlan(step domain.Step, steps []domain.Step) bool {
	return slices.Contains(steps, step)
}
