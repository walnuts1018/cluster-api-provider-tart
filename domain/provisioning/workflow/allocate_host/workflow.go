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
	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/hostallocation"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type Workflow struct {
	ports Ports
}

type Command struct {
	Machine *infrastructurev1beta1.TartMachine
}

// ReconcileResultはHostを確保した場合と待機する場合を閉じた集合で表す。
// go-sumtype:decl ReconcileResult
type ReconcileResult interface {
	isReconcileResult()
}

type HostAllocated struct {
	Host   *infrastructurev1beta1.TartHost
	Events []domain.Event
}

type AllocationPending struct {
	Failure FailurePresentation
}

func (HostAllocated) isReconcileResult()     {}
func (AllocationPending) isReconcileResult() {}
func (HostAllocated) isEvent()               {}
func (AllocationPending) isEvent()           {}

type Event interface{ isEvent() }

func NewWorkflow(ports Ports) *Workflow {
	return &Workflow{ports: ports}
}

func (workflow *Workflow) Do(ctx context.Context, command Command) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "allocate host", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](result.(Event))
}

func (workflow *Workflow) execute(ctx context.Context, input Command) (ReconcileResult, error) {
	for range 3 {
		command, err := CommandFromMachine(input.Machine)
		if err != nil {
			return nil, fmt.Errorf("parse host allocation input: %w", err)
		}

		candidates, err := workflow.ports.Candidates.ListCandidates(ctx, input.Machine)
		if err != nil {
			return nil, fmt.Errorf("list host candidates: %w", err)
		}
		command.Candidates = candidates

		decision := domain.Decide(command)
		if allocated, present := decision.Value().Value(); present {
			reservation, err := workflow.ports.Reservations.ReserveCandidate(ctx, input.Machine, allocated.Host)
			if err != nil {
				return nil, fmt.Errorf("reserve host candidate: %w", err)
			}
			switch reservation := reservation.(type) {
			case Reserved:
				return HostAllocated{
					Host:   reservation.Host,
					Events: allocated.Events,
				}, nil
			case RetrySelection:
				continue
			default:
				panic(fmt.Sprintf("unknown reservation result: %T", reservation))
			}
		}
		failure, present := decision.FailureValue().Value()
		if !present {
			panic("host allocation decision has no success or failure")
		}
		return AllocationPending{Failure: PresentFailure(failure)}, nil
	}

	return nil, fmt.Errorf("reserve host candidate: retry budget exhausted")
}
