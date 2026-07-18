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

package completeprovisioning

import (
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type OperationService interface {
	CompleteProvision(context.Context, *infrastructurev1beta1.TartHostOperation) error
}

type HostPhaseService interface {
	MarkHostProvisioned(context.Context, *infrastructurev1beta1.TartHost) error
}

type Command struct {
	Host      *infrastructurev1beta1.TartHost
	Operation *infrastructurev1beta1.TartHostOperation
}

type Event struct{}

type Workflow struct {
	operations OperationService
	hostPhase  HostPhaseService
}

func NewWorkflow(operations OperationService, hostPhase HostPhaseService) *Workflow {
	return &Workflow{operations: operations, hostPhase: hostPhase}
}

func (workflow *Workflow) Do(ctx context.Context, command Command) sharedresult.Result[Event, sharedworkflow.Failure] {
	if err := workflow.execute(ctx, command); err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "complete provisioning", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](Event{})
}

func (workflow *Workflow) execute(ctx context.Context, command Command) error {
	if err := workflow.operations.CompleteProvision(ctx, command.Operation); err != nil {
		return fmt.Errorf("complete Provision operation: %w", err)
	}
	if err := workflow.hostPhase.MarkHostProvisioned(ctx, command.Host); err != nil {
		return fmt.Errorf("mark TartHost provisioned: %w", err)
	}
	return nil
}
