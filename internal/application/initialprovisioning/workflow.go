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

package initialprovisioning

import (
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationhostallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/hostallocation"
	initialprovisioningevent "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/event"
	initialprovisioningmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/model"
	initialprovisioningport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/port"
	domainhostallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

var (
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
	if err := workflow.operations.CompleteProvision(ctx, operation); err != nil {
		return fmt.Errorf("complete Provision operation: %w", err)
	}
	if err := workflow.hostPhase.MarkHostProvisioned(ctx, host); err != nil {
		return fmt.Errorf("mark TartHost provisioned: %w", err)
	}
	return nil
}

type SessionTokenIssuer = initialprovisioningport.SessionTokenIssuer

type Event = initialprovisioningevent.Event
type EventHostReserved = initialprovisioningevent.HostReserved
type EventOperationStarted = initialprovisioningevent.OperationStarted
type EventProvisioningCompleted = initialprovisioningevent.ProvisioningCompleted

type StartResult = initialprovisioningmodel.StartResult
type Started = initialprovisioningmodel.Started
type AllocationPending = initialprovisioningmodel.AllocationPending

// Workflow はv1beta1 TartMachineの初期Provisioningを組み立てる。
type Workflow struct {
	hostAllocation *applicationhostallocation.Workflow
	hostPhase      HostPhaseService
	operations     OperationService
}

// NewWorkflow は初期Provisioning Workflowを生成する。
func NewWorkflow(
	hostReserve HostReserveService,
	hostPhase HostPhaseService,
	operations OperationService,
) *Workflow {
	return &Workflow{
		hostAllocation: applicationhostallocation.NewWorkflow(applicationhostallocation.Ports{
			Candidates:   hostReserve,
			Reservations: hostReserve,
		}),
		hostPhase:  hostPhase,
		operations: operations,
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
	allocationResult, err := workflow.hostAllocation.Reconcile(ctx, applicationhostallocation.ReconcileInput{
		Machine: machine,
	})
	if err != nil {
		return nil, fmt.Errorf("reconcile host allocation: %w", err)
	}
	if allocationResult.Failure != nil {
		return AllocationPending{
			Reason:       allocationResult.Failure.Reason,
			Message:      allocationResult.Failure.Message,
			RequeueAfter: allocationResult.Failure.RequeueAfter,
		}, nil
	}
	if allocationResult.Host == nil {
		return nil, fmt.Errorf("host allocation returned no host without failure")
	}
	if err := workflow.hostPhase.ReserveForMachine(ctx, allocationResult.Host, machine); err != nil {
		return nil, fmt.Errorf("mark TartHost reserved: %w", err)
	}

	desired, err := BuildOperationDraft(machine, allocationResult.Host, planDigest)
	if err != nil {
		return nil, err
	}
	operation, err := workflow.operations.Start(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("start TartHostOperation: %w", err)
	}
	return Started{
		Host:      allocationResult.Host,
		Operation: operation,
		Events: []initialprovisioningevent.Event{
			initialprovisioningevent.HostReserved{HostName: allocationResult.Host.Name},
			initialprovisioningevent.OperationStarted{OperationID: operation.Spec.OperationID},
		},
	}, nil
}

func BuildOperationDraft(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	planDigest string,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return buildOperationDraft(machine, host, planDigest)
}

// requirementsForMachine はTartMachineからAllocation Requirementsを構築する。
func requirementsForMachine(machine *infrastructurev1beta1.TartMachine) (domainhostallocation.Requirements, error) {
	return applicationhostallocation.RequirementsForMachine(machine)
}

// desiredObjectsDigest は初期Provisioning入力をRFC 8785 Canonical JSONで固定する。
// CAPI MachineとBootstrap SecretはPlan生成時に別途追加する。
func desiredObjectsDigest(machine *infrastructurev1beta1.TartMachine) (string, error) {
	return DesiredObjectsDigest(machine)
}
