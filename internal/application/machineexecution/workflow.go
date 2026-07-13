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

	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	applicationallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineallocation"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

const placeholderPlanDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

type HostReferenceService interface {
	EnsureMachineHostReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (allocationdomain.ReferenceResult, error)
}

type ProvisionWorkflow interface {
	Start(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		planDigest string,
	) (appprovisioning.StartResult, error)
	CompleteProvisioning(
		ctx context.Context,
		host *infrastructurev1beta1.TartHost,
		operation *infrastructurev1beta1.TartHostOperation,
	) error
}

type NodeHealthObserver interface {
	Observe(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machinehealthdomain.NodeObservation, bool, error)
}

type Workflow struct {
	client.Client
	HostReferences HostReferenceService
	NodeHealth     NodeHealthObserver
	Provisioner    ProvisionWorkflow
	Recorder       record.EventRecorder
}

func NewWorkflow(
	k8sClient client.Client,
	hostReferences HostReferenceService,
	nodeHealth NodeHealthObserver,
	provisioner ProvisionWorkflow,
	recorder record.EventRecorder,
) *Workflow {
	return &Workflow{
		Client:         k8sClient,
		HostReferences: hostReferences,
		NodeHealth:     nodeHealth,
		Provisioner:    provisioner,
		Recorder:       recorder,
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	switch machinelifecycledomain.DecideMachine(machineState(machine)) {
	case machinelifecycledomain.CommandObserveProvisionedMachine{}:
		updateHandled, err := workflow.reconcileUpdateOperation(ctx, machine)
		if err != nil {
			return err
		}
		if updateHandled {
			return nil
		}
		return workflow.reconcileNodeHealth(ctx, machine)
	case machinelifecycledomain.CommandEnsureProvisionReference{}:
	default:
		return fmt.Errorf("unknown TartMachine command")
	}

	shouldContinue, err := workflow.ensureProvisionReference(ctx, machine)
	if err != nil {
		return err
	}
	if !shouldContinue {
		return nil
	}

	switch machinelifecycledomain.DecideProvision(machineState(machine)) {
	case machinelifecycledomain.CommandStartProvision{}:
		if err := workflow.reconcileProvisionStart(ctx, machine); err != nil {
			return err
		}
	case machinelifecycledomain.CommandResumeProvisionOperation{}:
		if err := workflow.reconcileOperation(ctx, machine); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown TartMachine provision command")
	}

	return workflow.reconcileNodeHealth(ctx, machine)
}

func (workflow *Workflow) ensureProvisionReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (bool, error) {
	log := logf.FromContext(ctx)
	if workflow.HostReferences == nil {
		return false, fmt.Errorf("ensure TartMachine host reference: HostReferences is not configured")
	}
	result, err := workflow.HostReferences.EnsureMachineHostReference(ctx, machine)
	if errors.Is(err, allocationdomain.ErrConflict) {
		original := machine.DeepCopy()
		machine.Status = applicationallocation.StatusWithAllocationConflict(machine, err.Error())
		if patchErr := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return false, fmt.Errorf("set TartMachine AllocationConflict condition: %w", patchErr)
		}
		log.Info("TartMachine allocation conflict detected",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"error", err.Error(),
		)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ensure TartMachine host reference: %w", err)
	}
	if result == allocationdomain.ReferenceRepaired {
		log.Info("Repaired TartMachine host reference", "machine", client.ObjectKeyFromObject(machine).String())
	}
	return true, nil
}

func (workflow *Workflow) reconcileUpdateOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (bool, error) {
	operation, ok, err := workflow.referencedOperation(ctx, machine, "update outcome")
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if !ok || operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
		return false, nil
	}

	command, err := operationCommand(true, operation)
	if err != nil {
		return false, fmt.Errorf("decide Update TartHostOperation outcome: %w", err)
	}
	updateTerminal, ok := command.(machinelifecycledomain.CommandApplyUpdateTerminal)
	if !ok {
		return false, nil
	}
	if err := workflow.applyUpdateTerminal(ctx, machine, operation, updateTerminal.Outcome); err != nil {
		return false, err
	}
	return true, nil
}

func (workflow *Workflow) applyUpdateTerminal(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	outcome machinelifecycledomain.UpdateOutcome,
) error {
	original := machine.DeepCopy()
	switch outcome {
	case machinelifecycledomain.UpdateOutcomeSucceeded:
		machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "succeeded")
	case machinelifecycledomain.UpdateOutcomeRolledBack:
		machine.Status = appupdate.StatusWithUpdateRolledBack(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "failed")
	case machinelifecycledomain.UpdateOutcomeRecoveryRequired:
		machine.Status = appupdate.StatusWithUpdateRecoveryRequired(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "recovery_required")
	default:
		return fmt.Errorf("unknown TartMachine update outcome: %q", outcome)
	}
	if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Update status: %w", err)
	}
	if outcome == machinelifecycledomain.UpdateOutcomeRolledBack ||
		outcome == machinelifecycledomain.UpdateOutcomeRecoveryRequired {
		trace.SpanFromContext(ctx).SetAttributes(appupdate.FailureTraceAttributes(operation)...)
		workflow.recordUpdateFailureEvent(machine, operation)
	}
	return nil
}

func (workflow *Workflow) reconcileProvisionStart(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
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

func (workflow *Workflow) reconcileOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	log := logf.FromContext(ctx)

	operation, ok, err := workflow.referencedOperation(ctx, machine, "provision progress")
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Referenced TartHostOperation not found, clearing OperationRef",
				"machine", client.ObjectKeyFromObject(machine).String(),
				"operation", operationKey(machine.Status.OperationRef).String(),
			)
			original := machine.DeepCopy()
			machine.Status.OperationRef = nil
			if patchErr := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
				return fmt.Errorf("clear stale OperationRef: %w", patchErr)
			}
			return nil
		}
		return err
	}
	if !ok {
		return nil
	}

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

func (workflow *Workflow) reconcileNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	if workflow.NodeHealth == nil {
		return nil
	}

	observation, observed, err := workflow.NodeHealth.Observe(ctx, machine)
	if err != nil {
		return fmt.Errorf("observe workload Node health: %w", err)
	}
	if !observed {
		return nil
	}

	original := machine.DeepCopy()
	state := machineState(machine)
	operation, hasOperation, err := workflow.referencedOperation(ctx, machine, "health gate")
	if err != nil {
		return err
	}
	if !state.Provisioned && hasOperation {
		if err := workflow.applyProvisionHealth(ctx, machine, operation, observation); err != nil {
			return err
		}
	} else if state.Provisioned && hasOperation {
		terminalHandled, err := workflow.applyUpdateHealth(ctx, machine, operation, observation)
		if err != nil {
			return err
		}
		if terminalHandled {
			return nil
		}
	} else {
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	}

	if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Node health condition: %w", err)
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
		if workflow.Provisioner == nil {
			return fmt.Errorf("complete Provisioning: Provisioner is not configured")
		}
		host, err := workflow.referencedHost(ctx, machine, "health gate")
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
	case machinelifecycledomain.CommandSetProvisionHealthPending:
		machine.Status = appprovisioning.StatusWithHealthGatePending(
			machine,
			command.Reason,
			command.Message,
		)
	default:
		return fmt.Errorf("unknown Provision health command: %T", command)
	}
	return nil
}

func (workflow *Workflow) applyUpdateHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (bool, error) {
	command, err := operationCommand(true, operation)
	if err != nil {
		return false, fmt.Errorf("decide Update health gate: %w", err)
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandObserveUpdateHealth:
		healthCommand := machinelifecycledomain.DecideUpdateHealth(machinehealthdomain.EvaluateNode(observation))
		switch healthCommand.(type) {
		case machinelifecycledomain.CommandCompleteUpdate:
			if err := workflow.transitionOperationPhase(
				ctx,
				operation,
				infrastructurev1beta1.TartHostOperationPhaseSucceeded,
			); err != nil {
				return false, err
			}
			machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
		case machinelifecycledomain.CommandRollbackUpdate:
			if err := workflow.transitionUpdateFailurePhase(
				ctx,
				operation,
				infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
				infrastructurev1beta1.TartHostOperationPhaseRollingBack,
			); err != nil {
				return false, err
			}
			machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
		default:
			return false, fmt.Errorf("unknown Update health command: %T", healthCommand)
		}
	case machinelifecycledomain.CommandObserveNodeHealth:
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	case machinelifecycledomain.CommandApplyUpdateTerminal:
		if err := workflow.applyUpdateTerminal(ctx, machine, operation, command.Outcome); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("unexpected provisioned TartMachine command: %T", command)
	}
	return false, nil
}

func machineState(machine *infrastructurev1beta1.TartMachine) machinelifecycledomain.MachineState {
	provisioned := machine.Status.Initialization.Provisioned != nil && *machine.Status.Initialization.Provisioned
	return machinelifecycledomain.MachineState{
		Provisioned:  provisioned,
		HasOperation: machine.Status.OperationRef != nil,
	}
}

func operationCommand(
	provisioned bool,
	operation *infrastructurev1beta1.TartHostOperation,
) (machinelifecycledomain.OperationCommand, error) {
	kind, err := operationdomain.ParseKind(string(operation.Spec.Type))
	if err != nil {
		return nil, err
	}
	var phase operationdomain.Phase
	if operation.Status.Phase != "" {
		phase, err = operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return nil, err
		}
	}
	return machinelifecycledomain.DecideOperation(
		machinelifecycledomain.MachineState{Provisioned: provisioned, HasOperation: true},
		machinelifecycledomain.OperationState{Kind: kind, Phase: phase},
	)
}

func (workflow *Workflow) referencedOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (*infrastructurev1beta1.TartHostOperation, bool, error) {
	if machine.Status.OperationRef == nil {
		return nil, false, nil
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	key := operationKey(machine.Status.OperationRef)
	if err := workflow.Get(ctx, key, operation); err != nil {
		return nil, false, fmt.Errorf("get TartHostOperation for %s: %w", purpose, err)
	}
	if operation.UID != machine.Status.OperationRef.UID {
		return nil, false, fmt.Errorf(
			"TartHostOperation UID mismatch for %s: expected %s, got %s",
			purpose,
			machine.Status.OperationRef.UID,
			operation.UID,
		)
	}
	return operation, true, nil
}

func (workflow *Workflow) referencedHost(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (*infrastructurev1beta1.TartHost, error) {
	if machine.Status.HostRef == nil {
		return nil, fmt.Errorf("TartHost reference is missing for %s", purpose)
	}
	host := &infrastructurev1beta1.TartHost{}
	hostKey := client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}
	if err := workflow.Get(ctx, hostKey, host); err != nil {
		return nil, fmt.Errorf("get TartHost for %s: %w", purpose, err)
	}
	if host.UID != machine.Status.HostRef.UID {
		return nil, fmt.Errorf(
			"TartHost UID mismatch for %s: expected %s, got %s",
			purpose,
			machine.Status.HostRef.UID,
			host.UID,
		)
	}
	return host, nil
}

func (workflow *Workflow) recordUpdateFailureEvent(
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) {
	if workflow.Recorder == nil {
		return
	}
	condition := appupdate.FailureCondition(operation)
	if condition == nil {
		return
	}
	workflow.Recorder.Eventf(
		machine,
		"Warning",
		"UpdateFailed",
		"operationID=%s host=%s operationType=%s failureReason=%s message=%s",
		operation.Spec.OperationID,
		operation.Spec.HostRef.Name,
		operation.Spec.Type,
		condition.Reason,
		condition.Message,
	)
}

func (workflow *Workflow) transitionOperationPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	if err := workflow.Status().Patch(ctx, operation, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartHostOperation phase: %w", err)
	}
	return nil
}

func (workflow *Workflow) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	appupdate.UpdateFailureCondition(&operation.Status, operation.Generation, failedPhase, target)
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	if err := workflow.Status().Patch(ctx, operation, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartHostOperation update failure phase: %w", err)
	}
	return nil
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

func operationKey(ref *infrastructurev1beta1.ResourceReference) types.NamespacedName {
	if ref == nil {
		return types.NamespacedName{}
	}
	return types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
}
