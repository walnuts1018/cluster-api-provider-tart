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

package machineexecution

import (
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinelifecycle"
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type Command struct {
	Machine *infrastructurev1beta1.TartMachine
}
type Event struct{}

func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	if err := workflow.execute(ctx, command.Machine); err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "reconcile machine", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](Event{})
}

func (workflow *Workflow) execute(ctx context.Context, machine *infrastructurev1beta1.TartMachine) error {
	if workflow.pipeline == nil {
		return fmt.Errorf("reconcile TartMachine execution: pipeline is not configured")
	}
	return workflow.reconcileActive(ctx, machineexecutionmodel.ActiveFromAPI(machine))
}

func (workflow *Workflow) reconcileActive(
	ctx context.Context,
	active activeMachine,
) error {
	return workflow.pipeline.run(ctx, active, machinelifecycledomain.DecideMachine(active.State))
}
