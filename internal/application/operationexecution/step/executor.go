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

package step

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	operationexecutionport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution/port"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	inplaceupdatedomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/inplaceupdate"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/slot"
)

const redfishDriverName = "redfish"

type Dependencies struct {
	PowerOn            operationexecutionport.PowerOnService
	PrepareBoot        operationexecutionport.BootPreparationService
	HostPhase          operationexecutionport.HostPhaseService
	Targets            operationexecutionport.DriverTargetBuilder
	DriverCapabilities operationexecutionport.DriverCapabilityObserver
	DriverPowerState   operationexecutionport.DriverPowerStateObserver
	DriverBootState    operationexecutionport.DriverBootStateObserver
}

type Executor struct {
	client.Client
	Dependencies
	now func() time.Time
}

func NewExecutor(k8sClient client.Client, dependencies Dependencies, now func() time.Time) *Executor {
	return &Executor{
		Client:       k8sClient,
		Dependencies: dependencies,
		now:          now,
	}
}

func (executor *Executor) Decide(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (operationdomain.Decision, error) {
	kind, err := operationdomain.ParseKind(string(operation.Spec.Type))
	if err != nil {
		return operationdomain.Decision{}, err
	}
	var phase operationdomain.Phase
	if operation.Status.Phase != "" {
		phase, err = operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return operationdomain.Decision{}, err
		}
	}
	cleaningPolicy, err := executor.OperationCleaningPolicy(ctx, operation)
	if err != nil {
		return operationdomain.Decision{}, err
	}
	return operationdomain.Process(operationdomain.ProcessInput{
		State: operationdomain.ProcessState{
			Kind:           kind,
			Phase:          phase,
			CleaningPolicy: cleaningPolicy,
			Deadline:       operation.Spec.Deadline.Time,
		},
		Now: executor.now(),
	})
}

func (executor *Executor) OperationCleaningPolicy(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (operationdomain.CleaningPolicy, error) {
	switch operation.Spec.Type {
	case infrastructurev1beta1.OperationTypeClean, infrastructurev1beta1.OperationTypeWipeAll:
		policy, err := executor.cleaningPolicy(ctx, operation)
		if err != nil {
			return operationdomain.CleaningPolicyUnspecified, err
		}
		return domainCleaningPolicy(policy), nil
	case infrastructurev1beta1.OperationTypeProvision,
		infrastructurev1beta1.OperationTypeUpdate,
		infrastructurev1beta1.OperationTypeRollback,
		infrastructurev1beta1.OperationTypeRecovery:
		return operationdomain.CleaningPolicyUnspecified, nil
	}
	return operationdomain.CleaningPolicyUnspecified, nil
}

func (executor *Executor) PrepareOperationBoot(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	hostCommand operationdomain.HostCommand,
) error {
	log := logf.FromContext(ctx)

	host, err := executor.getHost(ctx, operation)
	if err != nil {
		return err
	}
	operationID, err := operationdomain.ParseID(operation.Spec.OperationID)
	if err != nil {
		return fmt.Errorf("parse operation ID: %w", err)
	}
	target, err := executor.driverTarget(ctx, host)
	if err != nil {
		return err
	}
	powerDriverName, err := driverdomain.ParseName(host.Spec.Management.PowerDriver)
	if err != nil {
		return fmt.Errorf("parse power driver name: %w", err)
	}
	bootDriver := host.Spec.Management.BootDriver
	if bootDriver == "" {
		bootDriver = host.Spec.Management.PowerDriver
	}
	bootDriverName, err := driverdomain.ParseName(bootDriver)
	if err != nil {
		return fmt.Errorf("parse boot driver name: %w", err)
	}
	invocation := applicationdriver.Invocation{
		OperationType: string(operation.Spec.Type),
		Phase:         "PreparingBoot",
		Rollback:      false,
	}
	if err := executor.observeDriverCapabilities(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost driver capabilities",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost driver capabilities: %w", err)
	}
	if err := executor.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost power state",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := executor.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost boot state",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", bootDriverName,
		)
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	if err := executor.prepareBoot(ctx, host, bootDriverName, target, operationID, invocation); err != nil {
		log.Error(err, "Failed to prepare TartHost boot transport",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", bootDriverName,
		)
		return fmt.Errorf("prepare TartHost boot transport: %w", err)
	}
	if err := executor.PowerOn.PowerOn(ctx, powerDriverName, target, operationID, invocation); err != nil {
		log.Error(err, "Failed to power on TartHost for Operation",
			"operation", client.ObjectKeyFromObject(operation).String(),
			"host", operation.Spec.HostRef.Name,
			"driver", host.Spec.Management.PowerDriver,
		)
		return fmt.Errorf("power on TartHost: %w", err)
	}
	if err := executor.applyHostCommandToHost(ctx, operation, host, hostCommand); err != nil {
		log.Error(err, "Failed to mark TartHost for Operation",
			"host", client.ObjectKeyFromObject(host).String(),
		)
		return err
	}
	return executor.TransitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhasePreparingBoot)
}

func (executor *Executor) CompleteOperationWithHostCommand(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	hostCommand operationdomain.HostCommand,
	target operationdomain.Phase,
) error {
	if err := executor.ApplyHostCommand(ctx, operation, hostCommand); err != nil {
		return err
	}
	return executor.TransitionPhase(ctx, operation, apiOperationPhase(target))
}

func (executor *Executor) ApplyHostCommand(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	command operationdomain.HostCommand,
) error {
	if _, ok := command.(operationdomain.HostNoop); ok {
		return nil
	}
	host, err := executor.getHost(ctx, operation)
	if err != nil {
		return err
	}
	return executor.applyHostCommandToHost(ctx, operation, host, command)
}

func (executor *Executor) ApplyDeadlineOutcome(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	outcome operationdomain.DeadlineOutcome,
) error {
	switch selected := outcome.(type) {
	case operationdomain.DeadlineMarkFailed:
		if selected.WithUpdateFailure {
			return executor.transitionUpdateFailurePhase(ctx, operation, apiOperationPhase(selected.FailedPhase), infrastructurev1beta1.TartHostOperationPhaseFailed)
		}
		return executor.TransitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseFailed)
	case operationdomain.DeadlineRecordBootFailure:
		return executor.handleBootTrialDeadlineExceeded(ctx, operation)
	case operationdomain.DeadlineTransitionFailure:
		return executor.transitionUpdateFailurePhase(ctx, operation, apiOperationPhase(selected.FailedPhase), apiOperationPhase(selected.Target))
	default:
		return fmt.Errorf("unknown TartHostOperation deadline outcome %T", selected)
	}
}

func (executor *Executor) ObserveActiveOperationDriverState(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if executor.DriverBootState == nil && executor.DriverPowerState == nil {
		return nil
	}
	host, err := executor.getHost(ctx, operation)
	if err != nil {
		return err
	}
	target, err := executor.driverTarget(ctx, host)
	if err != nil {
		return err
	}
	bootDriver := host.Spec.Management.BootDriver
	if bootDriver == "" {
		bootDriver = host.Spec.Management.PowerDriver
	}
	bootDriverName, err := driverdomain.ParseName(bootDriver)
	if err != nil {
		return fmt.Errorf("parse boot driver name: %w", err)
	}
	invocation := applicationdriver.Invocation{
		OperationType: string(operation.Spec.Type),
		Phase:         string(operation.Status.Phase),
		Rollback:      operation.Status.Phase == infrastructurev1beta1.TartHostOperationPhaseRollingBack,
	}
	powerDriverName, err := driverdomain.ParseName(host.Spec.Management.PowerDriver)
	if err != nil {
		return fmt.Errorf("parse power driver name: %w", err)
	}
	if err := executor.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := executor.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	return nil
}

func (executor *Executor) TransitionPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := executor.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return executor.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (executor *Executor) applyHostCommandToHost(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	host *infrastructurev1beta1.TartHost,
	command operationdomain.HostCommand,
) error {
	switch selected := command.(type) {
	case operationdomain.HostNoop:
		return nil
	case operationdomain.HostMarkProvisioning:
		return executor.HostPhase.MarkHostProvisioning(ctx, host)
	case operationdomain.HostMarkUpdating:
		return executor.HostPhase.MarkHostUpdating(ctx, host)
	case operationdomain.HostMarkCleaning:
		return executor.HostPhase.MarkHostCleaningForDeletion(ctx, host, apiCleaningPolicy(selected.Policy))
	case operationdomain.HostMarkAvailable:
		return executor.HostPhase.MarkHostAvailable(ctx, host)
	case operationdomain.HostMarkRetained:
		return executor.HostPhase.MarkHostRetained(ctx, host)
	case operationdomain.HostMarkDetached:
		return executor.HostPhase.MarkHostDetached(ctx, host)
	case operationdomain.HostMarkProvisioned:
		return executor.HostPhase.MarkHostProvisioned(ctx, host)
	case operationdomain.HostMarkRecoveryRequired:
		return executor.HostPhase.MarkHostRecoveryRequired(ctx, host)
	default:
		return fmt.Errorf("unknown TartHostOperation host command %T for %s", selected, client.ObjectKeyFromObject(operation).String())
	}
}

func (executor *Executor) prepareBoot(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
	invocation applicationdriver.Invocation,
) error {
	if executor.PrepareBoot == nil || host.Spec.Management.BootDriver != redfishDriverName {
		return nil
	}
	preferred, ok, err := preferredBootTarget(host)
	if err != nil {
		return err
	}
	var targetOverride *driverdomain.BootTarget
	if ok {
		targetOverride = &preferred
	}
	_, err = executor.PrepareBoot.PrepareBoot(ctx, driverName, target, operationID, targetOverride, invocation)
	return err
}

func preferredBootTarget(host *infrastructurev1beta1.TartHost) (driverdomain.BootTarget, bool, error) {
	if host.Spec.Management.Redfish == nil {
		return "", false, nil
	}
	switch host.Spec.Management.Redfish.PreferredBootTransport {
	case "":
		return "", false, nil
	case infrastructurev1beta1.BootTransportRedfishHTTPBoot:
		return driverdomain.BootTargetHTTP, true, nil
	case infrastructurev1beta1.BootTransportRedfishPXE:
		return driverdomain.BootTargetPXE, true, nil
	case infrastructurev1beta1.BootTransportRedfishVirtualMedia:
		return driverdomain.BootTargetVirtualMedia, true, nil
	case infrastructurev1beta1.BootTransportIPXE:
		return "", false, fmt.Errorf("unsupported Redfish preferred boot transport %q", host.Spec.Management.Redfish.PreferredBootTransport)
	}
	return "", false, fmt.Errorf("unsupported Redfish preferred boot transport %q", host.Spec.Management.Redfish.PreferredBootTransport)
}

func (executor *Executor) driverTarget(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
) (driverdomain.HostTarget, error) {
	if executor.Targets != nil {
		return executor.Targets.Build(ctx, host)
	}
	bootMAC, err := driverdomain.ParseMACAddress(host.Spec.Identifiers.BootMACAddress)
	if err != nil {
		return driverdomain.HostTarget{}, fmt.Errorf("parse TartHost boot MAC address: %w", err)
	}
	return driverdomain.NewHostTarget(bootMAC), nil
}

func (executor *Executor) observeDriverCapabilities(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if executor.DriverCapabilities == nil {
		return nil
	}
	return executor.DriverCapabilities.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (executor *Executor) observeDriverPowerState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if executor.DriverPowerState == nil {
		return nil
	}
	return executor.DriverPowerState.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (executor *Executor) observeDriverBootState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if executor.DriverBootState == nil || host.Spec.Management.BootDriver != redfishDriverName {
		return nil
	}
	return executor.DriverBootState.ObserveBootAndPersist(ctx, driverName, target, host, invocation)
}

func (executor *Executor) cleaningPolicy(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (infrastructurev1beta1.DeletionPolicy, error) {
	if operation.Spec.Type == infrastructurev1beta1.OperationTypeWipeAll {
		return infrastructurev1beta1.DeletionPolicyWipeAll, nil
	}
	if operation.Spec.MachineRef == nil {
		return "", fmt.Errorf("machineRef is required for Clean operation")
	}
	machine := &infrastructurev1beta1.TartMachine{}
	if err := executor.Get(ctx, client.ObjectKey{
		Namespace: operation.Spec.MachineRef.Namespace,
		Name:      operation.Spec.MachineRef.Name,
	}, machine); err != nil {
		return "", fmt.Errorf("get TartMachine for Cleaning policy: %w", err)
	}
	if machine.UID != operation.Spec.MachineRef.UID {
		return "", fmt.Errorf("TartMachine UID mismatch: expected %s, got %s", operation.Spec.MachineRef.UID, machine.UID)
	}
	return machine.Spec.DeletionPolicy, nil
}

func (executor *Executor) getHost(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHost, error) {
	host := &infrastructurev1beta1.TartHost{}
	if err := executor.Get(ctx, client.ObjectKey{
		Namespace: operation.Spec.HostRef.Namespace,
		Name:      operation.Spec.HostRef.Name,
	}, host); err != nil {
		return nil, fmt.Errorf("get TartHost for Operation: %w", err)
	}
	if host.UID != operation.Spec.HostRef.UID {
		return nil, fmt.Errorf("TartHost UID mismatch: expected %s, got %s", operation.Spec.HostRef.UID, host.UID)
	}
	return host, nil
}

func (executor *Executor) handleBootTrialDeadlineExceeded(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	targetSlot, err := slotdomain.Parse(string(operation.Spec.TargetSlot))
	if err != nil {
		return err
	}
	activeSlot, err := targetSlot.Inactive()
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := executor.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		decision, err := inplaceupdatedomain.Transition(inplaceupdatedomain.State{
			Phase:      operationdomain.PhaseBootTrial,
			ActiveSlot: activeSlot,
			TargetSlot: targetSlot,
			Attempt:    current.Status.Attempt,
		}, inplaceupdatedomain.EventBootFailed)
		if err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Attempt = decision.Attempt
		current.Status.Phase = infrastructurev1beta1.TartHostOperationPhase(decision.Phase)
		appupdate.SetUpdateFailureCondition(
			&current.Status,
			current.Generation,
			infrastructurev1beta1.TartHostOperationPhaseBootTrial,
			current.Status.Phase,
		)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return executor.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (executor *Executor) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := executor.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		appupdate.UpdateFailureCondition(&current.Status, current.Generation, failedPhase, target)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return executor.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func domainCleaningPolicy(policy infrastructurev1beta1.DeletionPolicy) operationdomain.CleaningPolicy {
	switch policy {
	case infrastructurev1beta1.DeletionPolicyRetainData:
		return operationdomain.CleaningPolicyRetainData
	case infrastructurev1beta1.DeletionPolicyRetainState:
		return operationdomain.CleaningPolicyRetainState
	case infrastructurev1beta1.DeletionPolicyWipeAll:
		return operationdomain.CleaningPolicyWipeAll
	default:
		return operationdomain.CleaningPolicyUnspecified
	}
}

func apiCleaningPolicy(policy operationdomain.CleaningPolicy) infrastructurev1beta1.DeletionPolicy {
	switch policy {
	case operationdomain.CleaningPolicyUnspecified:
		return ""
	case operationdomain.CleaningPolicyRetainData:
		return infrastructurev1beta1.DeletionPolicyRetainData
	case operationdomain.CleaningPolicyRetainState:
		return infrastructurev1beta1.DeletionPolicyRetainState
	case operationdomain.CleaningPolicyWipeAll:
		return infrastructurev1beta1.DeletionPolicyWipeAll
	}
	return ""
}

func apiOperationPhase(phase operationdomain.Phase) infrastructurev1beta1.TartHostOperationPhase {
	return infrastructurev1beta1.TartHostOperationPhase(phase)
}
