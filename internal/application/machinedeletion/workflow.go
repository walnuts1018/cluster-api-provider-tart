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
	machinedeletionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion/model"
	machinedeletionport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion/port"
	machinedeletionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion/step"
	machinedeletiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinedeletion"
)

type CleaningStep = machinedeletionport.CleaningStep

type Result = machinedeletionmodel.Result
type ResultWaiting = machinedeletionmodel.ResultWaiting
type ResultFinalizerReady = machinedeletionmodel.ResultFinalizerReady

type Workflow struct {
	steps *machinedeletionstep.Executor
}

func NewWorkflow(k8sClient client.Client, cleaner CleaningStep) *Workflow {
	steps := machinedeletionstep.NewExecutor(k8sClient, cleaner)
	return &Workflow{
		steps: steps,
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	observation, err := workflow.steps.Observe(ctx, machine)
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
		return ResultWaiting{}, workflow.steps.StartCleaning(ctx, machine, host)
	case machinedeletiondomain.CommandClearOperationReference:
		return ResultWaiting{}, workflow.steps.ClearOperationReference(ctx, machine)
	case machinedeletiondomain.CommandWaitCleaning:
		return ResultWaiting{}, nil
	case machinedeletiondomain.CommandFailCleaning:
		return nil, fmt.Errorf("Cleaning operation finished in %s", command.Phase)
	default:
		return nil, fmt.Errorf("unknown TartMachine deletion command: %T", command)
	}
}
