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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
	domainhostallocation "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/hostallocation"
	initialprovisioningevent "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/event/initialprovisioning"
	hostallocation "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/allocate_host"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type PlanSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
}

type Workflow struct {
	hostAllocation *hostallocation.Workflow
	hostPhase      HostPhaseService
	operations     OperationService
	plans          PlanWriter
	signer         PlanSigner
}

type Command struct {
	Machine    *infrastructurev1beta1.TartMachine
	MachineUID string
	Manifest   artifact.ValidatedManifest
}

type Event interface{ isEvent() }

type MachineProvisioningStarted struct{ Result Started }
type HostAllocationPending struct{ Result AllocationPending }

func (MachineProvisioningStarted) isEvent() {}
func (HostAllocationPending) isEvent()      {}

func NewWorkflow(
	hostReserve HostReserveService,
	hostPhase HostPhaseService,
	operations OperationService,
	plans PlanWriter,
	signer PlanSigner,
) *Workflow {
	return &Workflow{
		hostAllocation: hostallocation.NewWorkflow(hostallocation.Ports{Candidates: hostReserve, Reservations: hostReserve}),
		hostPhase:      hostPhase,
		operations:     operations,
		plans:          plans,
		signer:         signer,
	}
}

func (workflow *Workflow) Do(ctx context.Context, command Command) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "provision machine", Detail: err.Error()})
	}
	switch result := result.(type) {
	case Started:
		return sharedworkflow.Succeeded[Event](MachineProvisioningStarted{Result: result})
	case AllocationPending:
		return sharedworkflow.Succeeded[Event](HostAllocationPending{Result: result})
	default:
		panic(fmt.Sprintf("unknown provisioning result: %T", result))
	}
}

func (workflow *Workflow) execute(ctx context.Context, command Command) (StartResult, error) {
	if command.Machine == nil {
		return nil, fmt.Errorf("TartMachine is required")
	}
	if command.MachineUID == "" {
		return nil, fmt.Errorf("CAPI Machine UID is required")
	}
	allocationOutcome := workflow.hostAllocation.Do(ctx, hostallocation.Command{Machine: command.Machine})
	allocationEvent, present := allocationOutcome.Value().Value()
	if !present {
		failure, _ := allocationOutcome.FailureValue().Value()
		return nil, fmt.Errorf("reconcile host allocation: %s", failure.Message())
	}
	switch event := allocationEvent.(type) {
	case hostallocation.AllocationPending:
		return AllocationPending{Reason: event.Failure.Reason, Message: event.Failure.Message, RequeueAfter: event.Failure.RequeueAfter}, nil
	case hostallocation.HostAllocated:
		return workflow.startOnAllocatedHost(ctx, command, event)
	default:
		panic(fmt.Sprintf("unknown host allocation event: %T", allocationEvent))
	}
}

func (workflow *Workflow) startOnAllocatedHost(ctx context.Context, command Command, allocation hostallocation.HostAllocated) (StartResult, error) {
	if err := workflow.hostPhase.ReserveForMachine(ctx, allocation.Host, command.Machine); err != nil {
		return nil, fmt.Errorf("mark TartHost reserved: %w", err)
	}
	deadline := metav1.NewTime(time.Now().UTC().Truncate(time.Second).Add(defaultOperationDeadline))
	operationID, err := deterministicOperationUID(allocation.Host, command.Machine)
	if err != nil {
		return nil, err
	}
	candidatePlan, err := BuildProvisionPlan(ProvisionPlanInput{OperationID: operationID, Host: allocation.Host, Machine: command.Machine, MachineUID: command.MachineUID, Deadline: deadline.Time, Manifest: command.Manifest}, workflow.signer.KeyID, workflow.signer.PrivateKey)
	if err != nil {
		return nil, err
	}
	desired, err := buildOperationDraftWithDeadline(command.Machine, allocation.Host, candidatePlan.Digest.String(), deadline)
	if err != nil {
		return nil, err
	}
	operation, err := workflow.operations.Start(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("start TartHostOperation: %w", err)
	}
	persistedPlan, err := BuildProvisionPlan(ProvisionPlanInput{OperationID: operation.Spec.OperationID, Host: allocation.Host, Machine: command.Machine, MachineUID: command.MachineUID, Deadline: operation.Spec.Deadline.Time, Manifest: command.Manifest}, workflow.signer.KeyID, workflow.signer.PrivateKey)
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
	return Started{Host: allocation.Host, Operation: operation, Events: []initialprovisioningevent.Event{
		initialprovisioningevent.HostReserved{HostName: allocation.Host.Name},
		initialprovisioningevent.OperationStarted{OperationID: operation.Spec.OperationID},
	}}, nil
}

func BuildOperationDraft(machine *infrastructurev1beta1.TartMachine, host *infrastructurev1beta1.TartHost, planDigest string) (*infrastructurev1beta1.TartHostOperation, error) {
	return buildOperationDraft(machine, host, planDigest)
}

func requirementsForMachine(machine *infrastructurev1beta1.TartMachine) (domainhostallocation.Requirements, error) {
	return hostallocation.RequirementsForMachine(machine)
}

func desiredObjectsDigest(machine *infrastructurev1beta1.TartMachine) (string, error) {
	return DesiredObjectsDigest(machine)
}
