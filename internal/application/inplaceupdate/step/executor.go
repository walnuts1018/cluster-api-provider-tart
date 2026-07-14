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
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	inplaceupdateport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/port"
)

type Executor struct {
	operations         inplaceupdateport.OperationStarter
	plans              inplaceupdateport.PlanWriter
	nodeLifecyclePlans inplaceupdateport.NodeLifecyclePlanWriter
}

func NewExecutor(
	operations inplaceupdateport.OperationStarter,
	plans inplaceupdateport.PlanWriter,
) *Executor {
	return &Executor{
		operations: operations,
		plans:      plans,
	}
}

func (executor *Executor) SetNodeLifecyclePlanWriter(plans inplaceupdateport.NodeLifecyclePlanWriter) {
	executor.nodeLifecyclePlans = plans
}

func (executor *Executor) StartOperation(
	ctx context.Context,
	command StartOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return executor.operations.Start(ctx, command.Operation)
}

func (executor *Executor) PersistAgentPlan(ctx context.Context, command PersistAgentPlan) error {
	return executor.plans.Write(ctx, command.Operation, command.Plan, command.Signature)
}

func (executor *Executor) PersistNodeLifecyclePlan(ctx context.Context, command PersistNodeLifecyclePlan) error {
	if executor.nodeLifecyclePlans == nil {
		return fmt.Errorf("Node Lifecycle Plan writer is required for KubernetesBinary update")
	}
	return executor.nodeLifecyclePlans.Write(ctx, command.Operation, command.Plan, command.Signature)
}
