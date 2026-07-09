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

package nodelifecycle

import (
	"context"
	"fmt"

	distribution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

// StepRunnerは署名検証後のPlanだけを実際のLifecycle実装へ渡す境界である。
type StepRunner interface {
	RunStep(context.Context, domain.Plan, domain.Step) (distribution.StepResult, error)
}

type Service struct {
	trustStore agentprotocol.TrustStore
	runner     StepRunner
}

func NewService(trustStore agentprotocol.TrustStore, runner StepRunner) *Service {
	return &Service{trustStore: trustStore, runner: runner}
}

// RunSignedStepは署名済みPlanだけを受理し、未署名または改ざん済みPlanを実行しない。
func (service *Service) RunSignedStep(
	ctx context.Context,
	signed SignedPlan,
	step domain.Step,
) (distribution.StepResult, error) {
	if service.trustStore == nil {
		return distribution.StepResult{}, fmt.Errorf("lifecycle plan trust store is required")
	}
	if service.runner == nil {
		return distribution.StepResult{}, fmt.Errorf("lifecycle step runner is required")
	}
	validated, err := ValidatePlan(signed.Plan)
	if err != nil {
		return distribution.StepResult{}, err
	}
	if err := VerifySignature(validated, signed.Signature, service.trustStore); err != nil {
		return distribution.StepResult{}, err
	}
	plan, err := validated.DomainPlan()
	if err != nil {
		return distribution.StepResult{}, err
	}
	if err := domain.ReadyForStep(plan, step); err != nil {
		return distribution.StepResult{}, err
	}
	return service.runner.RunStep(ctx, plan, step)
}
