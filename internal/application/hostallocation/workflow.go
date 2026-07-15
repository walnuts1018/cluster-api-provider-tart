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

package hostallocation

import (
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

type Workflow struct {
	ports Ports
}

type ReconcileInput struct {
	Machine *infrastructurev1beta1.TartMachine
}

type ReconcileResult struct {
	Host    *infrastructurev1beta1.TartHost
	Events  []domain.Event
	Failure *FailurePresentation
}

func NewWorkflow(ports Ports) *Workflow {
	return &Workflow{ports: ports}
}

func (workflow *Workflow) Reconcile(ctx context.Context, input ReconcileInput) (ReconcileResult, error) {
	for range 3 {
		command, err := CommandFromMachine(input.Machine)
		if err != nil {
			return ReconcileResult{}, fmt.Errorf("parse host allocation input: %w", err)
		}

		candidates, err := workflow.ports.Candidates.ListCandidates(ctx, input.Machine)
		if err != nil {
			return ReconcileResult{}, fmt.Errorf("list host candidates: %w", err)
		}
		command.Candidates = candidates

		switch result := domain.Decide(command).(type) {
		case domain.Allocated:
			reservation, err := workflow.ports.Reservations.ReserveCandidate(ctx, input.Machine, result.Host)
			if err != nil {
				return ReconcileResult{}, fmt.Errorf("reserve host candidate: %w", err)
			}
			switch reservation := reservation.(type) {
			case Reserved:
				return ReconcileResult{
					Host:   reservation.Host,
					Events: result.Events,
				}, nil
			case RetrySelection:
				continue
			default:
				panic(fmt.Sprintf("unknown reservation result: %T", reservation))
			}
		case domain.NotAllocated:
			presentation := PresentFailure(result.Failure)
			return ReconcileResult{Failure: &presentation}, nil
		default:
			panic(fmt.Sprintf("unknown host allocation result: %T", result))
		}
	}

	return ReconcileResult{}, fmt.Errorf("reserve host candidate: retry budget exhausted")
}
