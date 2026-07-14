// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package initialprovisioning

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	initialprovisioningevent "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/event"
	initialprovisioninghandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/handler"
	initialprovisioningmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/model"
	initialprovisioningport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/port"
	initialprovisioningstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/step"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
)

var (
	ErrNoAvailableHost   = initialprovisioningmodel.ErrNoAvailableHost
	ErrBootstrapNotReady = initialprovisioningmodel.ErrBootstrapNotReady
)

type HostReserveService = initialprovisioningport.HostReserveService
type HostPhaseService = initialprovisioningport.HostPhaseService
type OperationService = initialprovisioningport.OperationService

// CompleteProvisioning はOperationとHostを最終状態へ順に収束させる。
// Operationを先に完了させ、再試行時はSucceededを冪等に受け入れる。
func (workflow *Workflow) CompleteProvisioning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	return workflow.commands.CompleteProvisioning(ctx, host, operation)
}

type SessionTokenIssuer = initialprovisioningport.SessionTokenIssuer

type Step = initialprovisioningstep.Step
type StepReserveHost = initialprovisioningstep.ReserveHost
type StepMarkHostReserved = initialprovisioningstep.MarkHostReserved
type StepStartOperation = initialprovisioningstep.StartOperation
type StepCompleteOperation = initialprovisioningstep.CompleteOperation
type StepMarkHostProvisioned = initialprovisioningstep.MarkHostProvisioned

type Event = initialprovisioningevent.Event
type EventHostReserved = initialprovisioningevent.HostReserved
type EventOperationStarted = initialprovisioningevent.OperationStarted
type EventProvisioningCompleted = initialprovisioningevent.ProvisioningCompleted

type StartResult = initialprovisioningmodel.StartResult

// Workflow はv1beta1 TartMachineの初期Provisioningを組み立てる。
type Workflow struct {
	commands *initialprovisioninghandler.CommandHandler
}

// NewWorkflow は初期Provisioning Workflowを生成する。
func NewWorkflow(
	hostReserve HostReserveService,
	hostPhase HostPhaseService,
	operations OperationService,
) *Workflow {
	steps := initialprovisioningstep.NewExecutor(hostReserve, hostPhase, operations)
	return &Workflow{
		commands: initialprovisioninghandler.NewCommandHandler(steps),
	}
}

// Start はHostを選択・予約してProvision Operationを作成する。
//
// 冪等性: 同じmachine/hostのOperationが既に存在する場合は既存のOperationを返す。
// 呼び出し元はHostRef/OperationRefをStatus Patchで永続化する責務を持つ。
func (workflow *Workflow) Start(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	planDigest string,
) (StartResult, error) {
	return workflow.commands.Start(ctx, machine, planDigest)
}

func BuildOperationDraft(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	planDigest string,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return initialprovisioningstep.BuildOperationDraft(machine, host, planDigest)
}

// requirementsForMachine はTartMachineからAllocation Requirementsを構築する。
func requirementsForMachine(machine *infrastructurev1beta1.TartMachine) (allocationdomain.Requirements, error) {
	return initialprovisioningstep.RequirementsForMachine(machine)
}

// desiredObjectsDigest は初期Provisioning入力をRFC 8785 Canonical JSONで固定する。
// CAPI MachineとBootstrap SecretはPlan生成時に別途追加する。
func desiredObjectsDigest(machine *infrastructurev1beta1.TartMachine) (string, error) {
	return initialprovisioningstep.DesiredObjectsDigest(machine)
}
