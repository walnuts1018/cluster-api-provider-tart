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

package machinedeletion

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinedeletiondomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinedeletion"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type Command struct {
	Machine *infrastructurev1beta1.TartMachine
}
type Event struct{ Result Result }

type Workflow struct {
	runtime *runtime
}

func NewWorkflow(k8sClient client.Client, cleaner CleaningWorkflow) *Workflow {
	return &Workflow{runtime: newRuntime(k8sClient, cleaner)}
}

func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command.Machine)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "delete machine", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](Event{Result: result})
}

func (workflow *Workflow) execute(ctx context.Context, machine *infrastructurev1beta1.TartMachine) (Result, error) {
	observation, err := workflow.runtime.observe(ctx, machine)
	if err != nil {
		return nil, err
	}

	command, err := machinedeletiondomain.Decide(observation.State)
	if err != nil {
		return nil, err
	}
	return workflow.applyDecision(ctx, machine, observation.Host, command)
}

func (workflow *Workflow) applyDecision(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	command machinedeletiondomain.Command,
) (Result, error) {
	switch command := command.(type) {
	case machinedeletiondomain.CommandReleaseFinalizer:
		return ResultFinalizerReady{}, nil
	case machinedeletiondomain.CommandStartCleaning:
		if host == nil {
			return nil, fmt.Errorf("TartHost is required to start Cleaning operation")
		}
		return ResultWaiting{}, workflow.runtime.startCleaning(ctx, machine, host)
	case machinedeletiondomain.CommandClearOperationReference:
		return ResultWaiting{}, workflow.runtime.clearOperationReference(ctx, machine)
	case machinedeletiondomain.CommandWaitCleaning:
		return ResultWaiting{}, nil
	case machinedeletiondomain.CommandFailCleaning:
		return nil, fmt.Errorf("cleaning operation finished in %s", command.Phase)
	default:
		return nil, fmt.Errorf("unknown TartMachine deletion command: %T", command)
	}
}
