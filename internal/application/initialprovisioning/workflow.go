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
	"crypto/ed25519"
	"fmt"
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationhostallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/hostallocation"
	initialprovisioningevent "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/event"
	initialprovisioningmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/model"
	initialprovisioningport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/port"
	domainhostallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	ErrBootstrapNotReady = initialprovisioningmodel.ErrBootstrapNotReady
)

type HostReserveService = initialprovisioningport.HostReserveService
type HostPhaseService = initialprovisioningport.HostPhaseService
type OperationService = initialprovisioningport.OperationService
type PlanWriter = initialprovisioningport.PlanWriter

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

// PlanSigner は Provision Plan の署名鍵を保持する。
type PlanSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
}

type Event = initialprovisioningevent.Event
type EventHostReserved = initialprovisioningevent.HostReserved
type EventOperationStarted = initialprovisioningevent.OperationStarted
type EventProvisioningCompleted = initialprovisioningevent.ProvisioningCompleted

type StartResult = initialprovisioningmodel.StartResult
type Started = initialprovisioningmodel.Started
type AllocationPending = initialprovisioningmodel.AllocationPending

// Workflow は v1beta1 TartMachine の初期 Provisioning を組み立てる。
type Workflow struct {
	hostAllocation *applicationhostallocation.Workflow
	hostPhase      HostPhaseService
	operations     OperationService
	plans          PlanWriter
	signer         PlanSigner
}

// WorkflowInput は初期 Provisioning に必要な入力をまとめる。
type WorkflowInput struct {
	Machine    *infrastructurev1beta1.TartMachine
	MachineUID string
	Manifest   artifact.ValidatedManifest
}

// NewWorkflow は初期 Provisioning Workflow を生成する。
func NewWorkflow(
	hostReserve HostReserveService,
	hostPhase HostPhaseService,
	operations OperationService,
	plans PlanWriter,
	signer PlanSigner,
) *Workflow {
	return &Workflow{
		hostAllocation: applicationhostallocation.NewWorkflow(applicationhostallocation.Ports{
			Candidates:   hostReserve,
			Reservations: hostReserve,
		}),
		hostPhase:  hostPhase,
		operations: operations,
		plans:      plans,
		signer:     signer,
	}
}

// Start は Host を選択・予約し、Operation 作成後に署名済み Provision Plan を保存する。
//
// 冪等性: 同じmachine/hostのOperationが既に存在する場合は既存のOperationを返す。
// 呼び出し元はHostRef/OperationRefをStatus Patchで永続化する責務を持つ。
func (workflow *Workflow) Start(ctx context.Context, input WorkflowInput) (StartResult, error) {
	if input.Machine == nil {
		return nil, fmt.Errorf("TartMachine is required")
	}
	if input.MachineUID == "" {
		return nil, fmt.Errorf("CAPI Machine UID is required")
	}
	allocationResult, err := workflow.hostAllocation.Reconcile(ctx, applicationhostallocation.ReconcileInput{
		Machine: input.Machine,
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
	if err := workflow.hostPhase.ReserveForMachine(ctx, allocationResult.Host, input.Machine); err != nil {
		return nil, fmt.Errorf("mark TartHost reserved: %w", err)
	}

	deadline := metav1.NewTime(time.Now().UTC().Truncate(time.Second).Add(defaultOperationDeadline))
	operationID, err := deterministicOperationUID(allocationResult.Host, input.Machine)
	if err != nil {
		return nil, err
	}
	candidatePlan, err := BuildProvisionPlan(ProvisionPlanInput{
		OperationID: operationID,
		Host:        allocationResult.Host,
		Machine:     input.Machine,
		MachineUID:  input.MachineUID,
		Deadline:    deadline.Time,
		Manifest:    input.Manifest,
	}, workflow.signer.KeyID, workflow.signer.PrivateKey)
	if err != nil {
		return nil, err
	}
	desired, err := buildOperationDraftWithDeadline(input.Machine, allocationResult.Host, candidatePlan.Digest.String(), deadline)
	if err != nil {
		return nil, err
	}
	operation, err := workflow.operations.Start(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("start TartHostOperation: %w", err)
	}
	persistedPlan, err := BuildProvisionPlan(ProvisionPlanInput{
		OperationID: operation.Spec.OperationID,
		Host:        allocationResult.Host,
		Machine:     input.Machine,
		MachineUID:  input.MachineUID,
		Deadline:    operation.Spec.Deadline.Time,
		Manifest:    input.Manifest,
	}, workflow.signer.KeyID, workflow.signer.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("build persisted Provision Plan: %w", err)
	}
	if persistedPlan.Digest.String() != operation.Spec.PlanDigest {
		return nil, fmt.Errorf("stored Provision Operation Plan digest does not match regenerated Plan")
	}
	if workflow.plans == nil {
		return nil, fmt.Errorf("provision Plan writer is required")
	}
	if err := workflow.plans.Write(ctx, operation, persistedPlan.Plan, persistedPlan.Signature); err != nil {
		return nil, fmt.Errorf("persist Provision Plan: %w", err)
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
