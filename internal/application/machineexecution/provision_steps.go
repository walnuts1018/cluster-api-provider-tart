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
	"github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/step"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

type provisionStartDependencyResult interface {
	isProvisionStartDependencyResult()
}

type provisionStartDependencyUnavailable struct{}

type provisionStartDependencyAvailable struct {
	Provisioner ProvisionStep
}

func (provisionStartDependencyUnavailable) isProvisionStartDependencyResult() {}
func (provisionStartDependencyAvailable) isProvisionStartDependencyResult()   {}

type provisionCompletionDependencyResult interface {
	isProvisionCompletionDependencyResult()
}

type provisionCompletionDependencyAvailable struct {
	Provisioner ProvisionStep
}

type provisionCompletionDependencyMissing struct{}

func (provisionCompletionDependencyAvailable) isProvisionCompletionDependencyResult() {}
func (provisionCompletionDependencyMissing) isProvisionCompletionDependencyResult()   {}

func (steps *StepExecutor) ensureProvisionReferenceStep(
	ctx context.Context,
	provisioning provisioningMachine,
) (model.ProvisionReferenceResult, error) {
	machine := provisioning.Machine
	log := logf.FromContext(ctx)
	if steps.HostReferences == nil {
		return nil, fmt.Errorf("ensure TartMachine host reference: HostReferences is not configured")
	}
	result, err := steps.HostReferences.EnsureMachineHostReference(ctx, machine)
	if errors.Is(err, allocationdomain.ErrConflict) {
		original := machine.DeepCopy()
		machine.Status = applicationallocation.StatusWithAllocationConflict(machine, err.Error())
		if patchErr := steps.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return nil, fmt.Errorf("set TartMachine AllocationConflict condition: %w", patchErr)
		}
		log.Info("TartMachine allocation conflict detected",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"error", err.Error(),
		)
		return model.ProvisionReferenceBlocked{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ensure TartMachine host reference: %w", err)
	}
	if result == allocationdomain.ReferenceRepaired {
		log.Info("Repaired TartMachine host reference", "machine", client.ObjectKeyFromObject(machine).String())
	}
	return model.ProvisionReferenceReady{}, nil
}

func (steps *StepExecutor) startProvisionStep(
	ctx context.Context,
	provisioning provisioningMachine,
) error {
	machine := provisioning.Machine
	log := logf.FromContext(ctx)

	dependency := steps.resolveProvisionStartDependencyStep(ctx, machine)
	var provisioner ProvisionStep
	switch dependency := dependency.(type) {
	case provisionStartDependencyUnavailable:
		return nil
	case provisionStartDependencyAvailable:
		provisioner = dependency.Provisioner
	default:
		return fmt.Errorf("unknown Provision start dependency result: %T", dependency)
	}

	readiness, err := steps.checkBootstrapReadinessStep(ctx, machine)
	if err != nil {
		return fmt.Errorf("check bootstrap readiness: %w", err)
	}
	switch readiness.(type) {
	case model.BootstrapDataWaiting:
		log.V(4).Info("Bootstrap data not yet ready, waiting",
			"machine", client.ObjectKeyFromObject(machine).String(),
		)
		if err := steps.applyProvisionStartStatusPatchStep(ctx, machine, model.ProvisionStartStatusWaitingForBootstrap{}); err != nil {
			return err
		}
		return nil
	case model.BootstrapDataReady:
	default:
		return fmt.Errorf("unknown Bootstrap readiness result: %T", readiness)
	}

	reservation, err := steps.reserveProvisionHostStep(ctx, provisioner, machine)
	if err != nil {
		return err
	}
	switch reservation := reservation.(type) {
	case model.ProvisionHostReservationNoHost:
		log.V(4).Info("No available TartHost, will retry", "machine", client.ObjectKeyFromObject(machine).String())
		if err := steps.applyProvisionStartStatusPatchStep(ctx, machine, model.ProvisionStartStatusNoAvailableHost{}); err != nil {
			return err
		}
		return nil
	case model.ProvisionHostReservationStarted:
		started := reservation.Started
		if _, err := steps.ensureProviderIDStep(ctx, machine, started.Host); err != nil {
			return err
		}

		if err := steps.applyProvisionStartStatusPatchStep(ctx, machine, model.ProvisionStartStatusHostReserved{
			Host:      started.Host,
			Operation: started.Operation,
		}); err != nil {
			return err
		}

		log.Info("TartMachine host reserved and operation started",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"host", client.ObjectKeyFromObject(started.Host).String(),
			"operation", client.ObjectKeyFromObject(started.Operation).String(),
		)
	default:
		return fmt.Errorf("unknown Provision host reservation result: %T", reservation)
	}
	return nil
}

func (steps *StepExecutor) applyProvisionStartStatusPatchStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	patch model.ProvisionStartStatusPatch,
) error {
	result, err := steps.patchProvisionStartStatusStep(ctx, machine, patch)
	if err != nil {
		return err
	}
	switch result.(type) {
	case model.ProvisionStartStatusPatched:
		return nil
	default:
		return fmt.Errorf("unknown Provision start status patch result: %T", result)
	}
}

func (steps *StepExecutor) resolveProvisionStartDependencyStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) provisionStartDependencyResult {
	if steps.Provisioner != nil {
		return provisionStartDependencyAvailable{Provisioner: steps.Provisioner}
	}
	logf.FromContext(ctx).V(4).Info("Provisioner not configured, skipping provisioning",
		"machine", client.ObjectKeyFromObject(machine).String(),
	)
	return provisionStartDependencyUnavailable{}
}

func (steps *StepExecutor) checkBootstrapReadinessStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (model.BootstrapReadinessResult, error) {
	coreMachine, err := util.GetOwnerMachine(ctx, steps.Client, machine.ObjectMeta)
	if err != nil {
		return nil, fmt.Errorf("get owner Machine: %w", err)
	}
	if coreMachine == nil || coreMachine.Spec.Bootstrap.DataSecretName == nil {
		return model.BootstrapDataWaiting{}, nil
	}
	return model.BootstrapDataReady{}, nil
}

func (steps *StepExecutor) reserveProvisionHostStep(
	ctx context.Context,
	provisioner ProvisionStep,
	machine *infrastructurev1beta1.TartMachine,
) (model.ProvisionHostReservationResult, error) {
	started, err := provisioner.Start(ctx, machine, placeholderPlanDigest)
	if errors.Is(err, appprovisioning.ErrNoAvailableHost) {
		return model.ProvisionHostReservationNoHost{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reserve host and start operation: %w", err)
	}
	if started.Host == nil || started.Operation == nil {
		return nil, fmt.Errorf("reserve host and start operation: Provisioner returned incomplete start result")
	}
	return model.ProvisionHostReservationStarted{Started: started}, nil
}

func (steps *StepExecutor) ensureProviderIDStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) (model.ProviderIDStepResult, error) {
	expected := fmt.Sprintf("tart://%s", host.Name)
	if machine.Spec.ProviderID == expected {
		return model.ProviderIDAlreadySet{}, nil
	}
	if machine.Spec.ProviderID != "" {
		return nil, fmt.Errorf(
			"TartMachine providerID %q does not match reserved TartHost %q",
			machine.Spec.ProviderID,
			host.Name,
		)
	}
	original := machine.DeepCopy()
	machine.Spec.ProviderID = expected
	if err := steps.Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return nil, fmt.Errorf("set TartMachine providerID: %w", err)
	}
	return model.ProviderIDPatched{}, nil
}

func (steps *StepExecutor) patchProvisionStartStatusStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	patch model.ProvisionStartStatusPatch,
) (model.ProvisionStartStatusPatchResult, error) {
	original := machine.DeepCopy()
	switch patch := patch.(type) {
	case model.ProvisionStartStatusWaitingForBootstrap:
		machine.Status = appprovisioning.StatusWithWaitingForBootstrap(machine)
	case model.ProvisionStartStatusNoAvailableHost:
		machine.Status = appprovisioning.StatusWithNoAvailableHost(machine)
	case model.ProvisionStartStatusHostReserved:
		machine.Status = appprovisioning.StatusWithHostReserved(machine, patch.Host, patch.Operation)
	default:
		return nil, fmt.Errorf("unknown Provision start status patch: %T", patch)
	}
	if err := steps.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return nil, fmt.Errorf("patch Provision start status: %w", err)
	}
	return model.ProvisionStartStatusPatched{}, nil
}

func (steps *StepExecutor) resolveProvisionProgressReferenceStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (model.ProvisionProgressReferenceResult, error) {
	operationReference, err := steps.resolveOperationReferenceStep(ctx, machine, "provision progress")
	if err != nil {
		return nil, err
	}
	switch reference := operationReference.(type) {
	case model.OperationReferenceAbsent:
		return model.ProvisionProgressReferenceAbsent{}, nil
	case model.OperationReferenceStale:
		return model.ProvisionProgressReferenceStale(reference), nil
	case model.OperationReferenceResolved:
		return model.ProvisionProgressReferenceResolved(reference), nil
	default:
		return nil, fmt.Errorf("unknown Operation reference result for provision progress: %T", operationReference)
	}
}

func (steps *StepExecutor) clearStaleProvisionOperationReferenceStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	reference *infrastructurev1beta1.ResourceReference,
) (model.StaleProvisionOperationReferenceCleared, error) {
	original := machine.DeepCopy()
	machine.Status.OperationRef = nil
	if err := steps.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return model.StaleProvisionOperationReferenceCleared{}, fmt.Errorf("clear stale OperationRef: %w", err)
	}
	return model.StaleProvisionOperationReferenceCleared{Reference: reference}, nil
}

func (steps *StepExecutor) patchProvisionFailureStatusStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	failure model.ProvisionProgressFailed,
) (model.ProvisionFailureStatusPatchResult, error) {
	original := machine.DeepCopy()
	machine.Status = appprovisioning.StatusWithProvisionFailed(machine, failure.Reason, failure.Message)
	if err := steps.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return model.ProvisionFailureStatusPatchResult{}, fmt.Errorf("set provision failed status: %w", err)
	}
	return model.ProvisionFailureStatusPatchResult(failure), nil
}

func (steps *StepExecutor) completeProvisionStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	dependency := steps.resolveProvisionCompletionDependencyStep()
	var provisioner ProvisionStep
	switch dependency := dependency.(type) {
	case provisionCompletionDependencyAvailable:
		provisioner = dependency.Provisioner
	case provisionCompletionDependencyMissing:
		return fmt.Errorf("complete Provisioning: Provisioner is not configured")
	default:
		return fmt.Errorf("unknown Provision completion dependency result: %T", dependency)
	}

	hostResult, err := steps.resolveProvisionCompletionHostStep(ctx, machine)
	if err != nil {
		return err
	}
	var host *infrastructurev1beta1.TartHost
	switch hostResult := hostResult.(type) {
	case model.ProvisionCompletionHostResolved:
		host = hostResult.Host
	default:
		return fmt.Errorf("unknown Provision completion host result: %T", hostResult)
	}

	completion, err := completeProvisionOperationStep(ctx, provisioner, host, operation)
	if err != nil {
		return err
	}
	switch completion.(type) {
	case model.ProvisionCompletionEffectApplied:
	default:
		return fmt.Errorf("unknown Provision completion effect result: %T", completion)
	}

	status := machineexecutionstep.PlanProvisionedStatus(machine, observation)
	switch status := status.(type) {
	case model.ProvisionedStatusPlanned:
		machine.Status = status.Status
	default:
		return fmt.Errorf("unknown Provisioned status result: %T", status)
	}
	return nil
}

func (steps *StepExecutor) resolveProvisionCompletionDependencyStep() provisionCompletionDependencyResult {
	if steps.Provisioner == nil {
		return provisionCompletionDependencyMissing{}
	}
	return provisionCompletionDependencyAvailable{Provisioner: steps.Provisioner}
}

func (steps *StepExecutor) resolveProvisionCompletionHostStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (model.ProvisionCompletionHostResult, error) {
	hostReference, err := steps.resolveHostReferenceStep(ctx, machine, "health gate")
	if err != nil {
		return nil, err
	}
	host, err := resolvedHost(hostReference, "health gate")
	if err != nil {
		return nil, err
	}
	return model.ProvisionCompletionHostResolved{Host: host}, nil
}

func completeProvisionOperationStep(
	ctx context.Context,
	provisioner ProvisionStep,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) (model.ProvisionCompletionEffectResult, error) {
	if err := provisioner.CompleteProvisioning(ctx, host, operation); err != nil {
		return nil, err
	}
	return model.ProvisionCompletionEffectApplied{}, nil
}

func (steps *StepExecutor) setProvisionHealthPendingStep(
	machine *infrastructurev1beta1.TartMachine,
	pending model.ProvisionHealthGatePending,
) {
	machine.Status = appprovisioning.StatusWithHealthGatePending(
		machine,
		pending.Reason,
		pending.Message,
	)
}
