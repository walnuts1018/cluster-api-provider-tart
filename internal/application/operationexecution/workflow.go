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

package operationexecution

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationexecutionhandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution/handler"
	operationexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution/model"
	operationexecutionport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution/port"
	operationexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution/step"
)

const DeadlineRequeueInterval = operationexecutionmodel.DeadlineRequeueInterval

type PowerOnService = operationexecutionport.PowerOnService
type BootPreparationService = operationexecutionport.BootPreparationService
type HostPhaseService = operationexecutionport.HostPhaseService
type DriverTargetBuilder = operationexecutionport.DriverTargetBuilder
type DriverCapabilityObserver = operationexecutionport.DriverCapabilityObserver
type DriverPowerStateObserver = operationexecutionport.DriverPowerStateObserver
type DriverBootStateObserver = operationexecutionport.DriverBootStateObserver

type Result = operationexecutionmodel.Result

// Workflow はOperation Process ManagerとCommand handlerだけを接続する。
type Workflow struct {
	steps    *operationexecutionstep.Executor
	commands *operationexecutionhandler.CommandHandler
}

func NewWorkflow(
	k8sClient client.Client,
	powerOn PowerOnService,
	prepareBoot BootPreparationService,
	hostPhase HostPhaseService,
	targets DriverTargetBuilder,
	driverCapabilities DriverCapabilityObserver,
	driverPowerState DriverPowerStateObserver,
	driverBootState DriverBootStateObserver,
) *Workflow {
	steps := operationexecutionstep.NewExecutor(
		k8sClient,
		operationexecutionstep.Dependencies{
			PowerOn:            powerOn,
			PrepareBoot:        prepareBoot,
			HostPhase:          hostPhase,
			Targets:            targets,
			DriverCapabilities: driverCapabilities,
			DriverPowerState:   driverPowerState,
			DriverBootState:    driverBootState,
		},
		defaultNow,
	)
	return &Workflow{
		steps:    steps,
		commands: operationexecutionhandler.NewCommandHandler(steps),
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (Result, error) {
	decision, err := workflow.steps.Decide(ctx, operation)
	if err != nil {
		return Result{}, err
	}
	return workflow.commands.Handle(ctx, operation, decision.Command)
}

func defaultNow() time.Time {
	return time.Now()
}
