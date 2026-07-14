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
	k8soperation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/operation"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type Workflow struct {
	ports   Ports
	effects *effectRunner
	now     func() time.Time
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
	ports := Ports{
		Resources:          k8soperation.NewReferenceReader(k8sClient),
		Statuses:           k8soperation.NewStatusWriter(k8sClient),
		PowerOn:            powerOn,
		PrepareBoot:        prepareBoot,
		HostPhase:          hostPhase,
		Targets:            targets,
		DriverCapabilities: driverCapabilities,
		DriverPowerState:   driverPowerState,
		DriverBootState:    driverBootState,
	}
	return &Workflow{
		ports:   ports,
		effects: &effectRunner{ports: ports},
		now:     time.Now,
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (Result, error) {
	command, err := mapCommand(ctx, workflow.ports.Resources, operation, workflow.now())
	if err != nil {
		return Result{}, err
	}
	decision := operationdomain.Decide(command)
	if err := workflow.effects.apply(ctx, operation, decision); err != nil {
		return Result{}, err
	}
	return present(decision), nil
}
