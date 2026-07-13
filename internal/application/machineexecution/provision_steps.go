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
	"errors"
	"fmt"

	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	applicationallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineallocation"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

func (workflow *Workflow) ensureProvisionReferenceStep(
	ctx context.Context,
	provisioning provisioningMachine,
) (provisionReferenceResult, error) {
	machine := provisioning.Machine
	log := logf.FromContext(ctx)
	if workflow.HostReferences == nil {
		return nil, fmt.Errorf("ensure TartMachine host reference: HostReferences is not configured")
	}
	result, err := workflow.HostReferences.EnsureMachineHostReference(ctx, machine)
	if errors.Is(err, allocationdomain.ErrConflict) {
		original := machine.DeepCopy()
		machine.Status = applicationallocation.StatusWithAllocationConflict(machine, err.Error())
		if patchErr := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return nil, fmt.Errorf("set TartMachine AllocationConflict condition: %w", patchErr)
		}
		log.Info("TartMachine allocation conflict detected",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"error", err.Error(),
		)
		return provisionReferenceBlocked{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ensure TartMachine host reference: %w", err)
	}
	if result == allocationdomain.ReferenceRepaired {
		log.Info("Repaired TartMachine host reference", "machine", client.ObjectKeyFromObject(machine).String())
	}
	return provisionReferenceReady{}, nil
}

func (workflow *Workflow) startProvisionStep(
	ctx context.Context,
	provisioning provisioningMachine,
) error {
	machine := provisioning.Machine
	log := logf.FromContext(ctx)

	if workflow.Provisioner == nil {
		log.V(4).Info("Provisioner not configured, skipping provisioning",
			"machine", client.ObjectKeyFromObject(machine).String(),
		)
		return nil
	}

	bootstrapReady, err := workflow.isBootstrapReady(ctx, machine)
	if err != nil {
		return fmt.Errorf("check bootstrap readiness: %w", err)
	}
	if !bootstrapReady {
		log.V(4).Info("Bootstrap data not yet ready, waiting",
			"machine", client.ObjectKeyFromObject(machine).String(),
		)
		original := machine.DeepCopy()
		machine.Status = appprovisioning.StatusWithWaitingForBootstrap(machine)
		if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("set WaitingForBootstrap status: %w", err)
		}
		return nil
	}

	started, err := workflow.Provisioner.Start(ctx, machine, placeholderPlanDigest)
	if errors.Is(err, appprovisioning.ErrNoAvailableHost) {
		log.V(4).Info("No available TartHost, will retry", "machine", client.ObjectKeyFromObject(machine).String())
		original := machine.DeepCopy()
		machine.Status = appprovisioning.StatusWithNoAvailableHost(machine)
		if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("set NoAvailableHost status: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("reserve host and start operation: %w", err)
	}
	if err := workflow.ensureProviderID(ctx, machine, started.Host); err != nil {
		return err
	}

	original := machine.DeepCopy()
	machine.Status = appprovisioning.StatusWithHostReserved(machine, started.Host, started.Operation)
	if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine HostRef/OperationRef: %w", err)
	}

	log.Info("TartMachine host reserved and operation started",
		"machine", client.ObjectKeyFromObject(machine).String(),
		"host", client.ObjectKeyFromObject(started.Host).String(),
		"operation", client.ObjectKeyFromObject(started.Operation).String(),
	)
	return nil
}

func (workflow *Workflow) ensureProviderID(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) error {
	expected := fmt.Sprintf("tart://%s", host.Name)
	if machine.Spec.ProviderID == expected {
		return nil
	}
	if machine.Spec.ProviderID != "" {
		return fmt.Errorf(
			"TartMachine providerID %q does not match reserved TartHost %q",
			machine.Spec.ProviderID,
			host.Name,
		)
	}
	original := machine.DeepCopy()
	machine.Spec.ProviderID = expected
	if err := workflow.Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine providerID: %w", err)
	}
	return nil
}

func (workflow *Workflow) resumeProvisionOperationStep(
	ctx context.Context,
	provisioning provisioningMachine,
) error {
	machine := provisioning.Machine
	log := logf.FromContext(ctx)

	operationReference, err := workflow.resolveOperationReferenceStep(ctx, machine, "provision progress")
	if err != nil {
		return err
	}
	switch reference := operationReference.(type) {
	case operationReferenceStale:
		log.Info("Referenced TartHostOperation not found, clearing OperationRef",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"operation", operationKey(reference.Reference).String(),
		)
		original := machine.DeepCopy()
		machine.Status.OperationRef = nil
		if patchErr := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return fmt.Errorf("clear stale OperationRef: %w", patchErr)
		}
		return nil
	case operationReferenceAbsent:
		return nil
	case operationReferenceResolved:
		return workflow.resumeResolvedProvisionOperationStep(ctx, machine, reference.Operation)
	default:
		return fmt.Errorf("unknown Operation reference result for provision progress: %T", operationReference)
	}
}

func (workflow *Workflow) resumeResolvedProvisionOperationStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	log := logf.FromContext(ctx)
	command, err := operationCommand(false, operation)
	if err != nil {
		return fmt.Errorf("decide TartHostOperation progress: %w", err)
	}

	switch command := command.(type) {
	case machinelifecycledomain.CommandMarkProvisionFailed:
		log.Info("TartHostOperation failed",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"operation", client.ObjectKeyFromObject(operation).String(),
			"phase", operation.Status.Phase,
		)
		original := machine.DeepCopy()
		machine.Status = appprovisioning.StatusWithProvisionFailed(machine,
			command.Reason,
			fmt.Sprintf("TartHostOperation %s/%s %s", operation.Namespace, operation.Name, operation.Status.Phase),
		)
		if patchErr := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return fmt.Errorf("set provision failed status: %w", patchErr)
		}
	case machinelifecycledomain.CommandObserveProvisionHealth:
		return nil
	default:
		return fmt.Errorf("unexpected TartMachine provisioning command: %T", command)
	}

	return nil
}

func (workflow *Workflow) applyProvisionHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	readiness := appprovisioning.EvaluateReadiness(operation, observation)
	command := machinelifecycledomain.DecideProvisionHealth(machinelifecycledomain.Readiness{
		Ready:   readiness.Ready,
		Reason:  readiness.Reason,
		Message: readiness.Message,
	})
	switch command := command.(type) {
	case machinelifecycledomain.CommandCompleteProvision:
		return workflow.completeProvisionStep(ctx, machine, operation, observation)
	case machinelifecycledomain.CommandSetProvisionHealthPending:
		workflow.setProvisionHealthPendingStep(machine, command)
	default:
		return fmt.Errorf("unknown Provision health command: %T", command)
	}
	return nil
}

func (workflow *Workflow) completeProvisionStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	if workflow.Provisioner == nil {
		return fmt.Errorf("complete Provisioning: Provisioner is not configured")
	}
	hostReference, err := workflow.resolveHostReferenceStep(ctx, machine, "health gate")
	if err != nil {
		return err
	}
	host, err := resolvedHost(hostReference, "health gate")
	if err != nil {
		return err
	}
	if err := workflow.Provisioner.CompleteProvisioning(ctx, host, operation); err != nil {
		return err
	}
	machine.Status = appprovisioning.StatusWithProvisioned(
		machine,
		machine.Status.Addresses,
		observation.ExpectedVersion,
	)
	return nil
}

func (workflow *Workflow) setProvisionHealthPendingStep(
	machine *infrastructurev1beta1.TartMachine,
	command machinelifecycledomain.CommandSetProvisionHealthPending,
) {
	machine.Status = appprovisioning.StatusWithHealthGatePending(
		machine,
		command.Reason,
		command.Message,
	)
}

func (workflow *Workflow) isBootstrapReady(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (bool, error) {
	coreMachine, err := util.GetOwnerMachine(ctx, workflow.Client, machine.ObjectMeta)
	if err != nil {
		return false, fmt.Errorf("get owner Machine: %w", err)
	}
	if coreMachine == nil {
		return false, nil
	}
	return coreMachine.Spec.Bootstrap.DataSecretName != nil, nil
}
