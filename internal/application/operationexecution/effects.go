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
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

const redfishDriverName = "redfish"

type effectRunner struct {
	ports Ports
}

func (runner *effectRunner) apply(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	result operationdomain.Result,
) error {
	switch selected := result.(type) {
	case operationdomain.InitializePending:
		return runner.ports.Statuses.TransitionPhase(ctx, operation, apiOperationPhase(selected.Target))
	case operationdomain.PrepareBoot:
		return runner.prepareBoot(ctx, operation, selected.Host)
	case operationdomain.ActivateBoot:
		return runner.activateBoot(ctx, operation)
	case operationdomain.ObserveActive:
		return runner.observeActive(ctx, operation)
	case operationdomain.AwaitMachineHealth:
		return nil
	case operationdomain.CompleteOperation:
		if err := runner.applyHostCommand(ctx, operation, selected.Host); err != nil {
			return err
		}
		return runner.ports.Statuses.TransitionPhase(ctx, operation, apiOperationPhase(selected.Target))
	case operationdomain.HandleTerminal:
		return runner.applyHostCommand(ctx, operation, selected.Host)
	case operationdomain.DeadlineExceeded:
		return runner.applyDeadlineOutcome(ctx, operation, selected.Outcome)
	case operationdomain.Ignore, operationdomain.Rejected:
		return nil
	default:
		return fmt.Errorf("unknown TartHostOperation workflow result %T", selected)
	}
}

func (runner *effectRunner) prepareBoot(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	hostCommand operationdomain.HostCommand,
) error {
	log := logf.FromContext(ctx)

	host, err := runner.ports.Resources.GetHost(ctx, operation.Spec.HostRef)
	if err != nil {
		return err
	}
	operationID, err := operationdomain.ParseID(operation.Spec.OperationID)
	if err != nil {
		return fmt.Errorf("parse operation ID: %w", err)
	}
	target, err := runner.driverTarget(ctx, host)
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
	if err := runner.observeDriverCapabilities(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost driver capabilities",
			"host", host.Namespace+"/"+host.Name,
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost driver capabilities: %w", err)
	}
	if err := runner.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost power state",
			"host", host.Namespace+"/"+host.Name,
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := runner.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost boot state",
			"host", host.Namespace+"/"+host.Name,
			"driver", bootDriverName,
		)
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	if err := runner.preparePreferredBoot(ctx, host, bootDriverName, target, operationID, invocation); err != nil {
		log.Error(err, "Failed to prepare TartHost boot transport",
			"host", host.Namespace+"/"+host.Name,
			"driver", bootDriverName,
		)
		return fmt.Errorf("prepare TartHost boot transport: %w", err)
	}
	if err := runner.applyHostCommandToHost(ctx, host, hostCommand); err != nil {
		log.Error(err, "Failed to mark TartHost for Operation",
			"host", host.Namespace+"/"+host.Name,
		)
		return err
	}
	return runner.ports.Statuses.TransitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhasePreparingBoot)
}

func (runner *effectRunner) activateBoot(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	log := logf.FromContext(ctx)
	host, err := runner.ports.Resources.GetHost(ctx, operation.Spec.HostRef)
	if err != nil {
		return err
	}
	operationID, err := operationdomain.ParseID(operation.Spec.OperationID)
	if err != nil {
		return fmt.Errorf("parse operation ID: %w", err)
	}
	target, err := runner.driverTarget(ctx, host)
	if err != nil {
		return err
	}
	powerDriverName, err := driverdomain.ParseName(host.Spec.Management.PowerDriver)
	if err != nil {
		return fmt.Errorf("parse power driver name: %w", err)
	}
	invocation := applicationdriver.Invocation{
		OperationType: string(operation.Spec.Type),
		Phase:         "PreparingBoot",
		Rollback:      false,
	}
	if err := runner.ports.PowerOn.PowerOn(ctx, powerDriverName, target, operationID, invocation); err != nil {
		log.Error(err, "Failed to power on TartHost for Operation",
			"operation", operation.Namespace+"/"+operation.Name,
			"host", operation.Spec.HostRef.Name,
			"driver", host.Spec.Management.PowerDriver,
		)
		return fmt.Errorf("power on TartHost: %w", err)
	}
	if err := runner.observeActive(ctx, operation); err != nil {
		return err
	}
	return runner.ports.Statuses.TransitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent)
}

func (runner *effectRunner) observeActive(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if runner.ports.DriverBootState == nil && runner.ports.DriverPowerState == nil {
		return nil
	}
	host, err := runner.ports.Resources.GetHost(ctx, operation.Spec.HostRef)
	if err != nil {
		return err
	}
	target, err := runner.driverTarget(ctx, host)
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
	if err := runner.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := runner.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	return nil
}

func (runner *effectRunner) applyHostCommand(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	command operationdomain.HostCommand,
) error {
	if _, ok := command.(operationdomain.HostNoop); ok {
		return nil
	}
	host, err := runner.ports.Resources.GetHost(ctx, operation.Spec.HostRef)
	if err != nil {
		return err
	}
	return runner.applyHostCommandToHost(ctx, host, command)
}

func (runner *effectRunner) applyHostCommandToHost(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	command operationdomain.HostCommand,
) error {
	switch selected := command.(type) {
	case operationdomain.HostNoop:
		return nil
	case operationdomain.HostMarkProvisioning:
		return runner.ports.HostPhase.MarkHostProvisioning(ctx, host)
	case operationdomain.HostMarkUpdating:
		return runner.ports.HostPhase.MarkHostUpdating(ctx, host)
	case operationdomain.HostMarkCleaning:
		return runner.ports.HostPhase.MarkHostCleaningForDeletion(ctx, host, apiCleaningPolicy(selected.Policy))
	case operationdomain.HostMarkAvailable:
		return runner.ports.HostPhase.MarkHostAvailable(ctx, host)
	case operationdomain.HostMarkRetained:
		return runner.ports.HostPhase.MarkHostRetained(ctx, host)
	case operationdomain.HostMarkDetached:
		return runner.ports.HostPhase.MarkHostDetached(ctx, host)
	case operationdomain.HostMarkProvisioned:
		return runner.ports.HostPhase.MarkHostProvisioned(ctx, host)
	case operationdomain.HostMarkRecoveryRequired:
		return runner.ports.HostPhase.MarkHostRecoveryRequired(ctx, host)
	default:
		return fmt.Errorf("unknown TartHostOperation host command %T", selected)
	}
}

func (runner *effectRunner) applyDeadlineOutcome(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	outcome operationdomain.DeadlineOutcome,
) error {
	switch selected := outcome.(type) {
	case operationdomain.DeadlineMarkFailed:
		if selected.WithUpdateFailure {
			return runner.ports.Statuses.TransitionUpdateFailure(
				ctx,
				operation,
				apiOperationPhase(selected.FailedPhase),
				infrastructurev1beta1.TartHostOperationPhaseFailed,
			)
		}
		return runner.ports.Statuses.TransitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseFailed)
	case operationdomain.DeadlineRecordBootFailure:
		return runner.ports.Statuses.HandleBootTrialDeadline(ctx, operation)
	case operationdomain.DeadlineTransitionFailure:
		return runner.ports.Statuses.TransitionUpdateFailure(
			ctx,
			operation,
			apiOperationPhase(selected.FailedPhase),
			apiOperationPhase(selected.Target),
		)
	default:
		return fmt.Errorf("unknown TartHostOperation deadline outcome %T", selected)
	}
}

func (runner *effectRunner) preparePreferredBoot(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
	invocation applicationdriver.Invocation,
) error {
	if runner.ports.PrepareBoot == nil || host.Spec.Management.BootDriver != redfishDriverName {
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
	_, err = runner.ports.PrepareBoot.PrepareBoot(ctx, driverName, target, operationID, targetOverride, invocation)
	return err
}

func preferredBootTarget(host *infrastructurev1beta1.TartHost) (driverdomain.BootTarget, bool, error) {
	if host.Spec.Management.Redfish == nil {
		return "", false, nil
	}
	//nolint:exhaustive // 空値とRedfishで有効なtransportだけがここでの分岐対象。
	switch host.Spec.Management.Redfish.PreferredBootTransport {
	case "":
		return "", false, nil
	case infrastructurev1beta1.BootTransportRedfishHTTPBoot:
		return driverdomain.BootTargetHTTP, true, nil
	case infrastructurev1beta1.BootTransportRedfishPXE:
		return driverdomain.BootTargetPXE, true, nil
	case infrastructurev1beta1.BootTransportRedfishVirtualMedia:
		return driverdomain.BootTargetVirtualMedia, true, nil
	default:
		return "", false, fmt.Errorf("unsupported Redfish preferred boot transport %q", host.Spec.Management.Redfish.PreferredBootTransport)
	}
}

func (runner *effectRunner) driverTarget(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
) (driverdomain.HostTarget, error) {
	if runner.ports.Targets != nil {
		return runner.ports.Targets.Build(ctx, host)
	}
	bootMAC, err := driverdomain.ParseMACAddress(host.Spec.Identifiers.BootMACAddress)
	if err != nil {
		return driverdomain.HostTarget{}, fmt.Errorf("parse TartHost boot MAC address: %w", err)
	}
	return driverdomain.NewHostTarget(bootMAC), nil
}

func (runner *effectRunner) observeDriverCapabilities(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if runner.ports.DriverCapabilities == nil {
		return nil
	}
	return runner.ports.DriverCapabilities.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (runner *effectRunner) observeDriverPowerState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if runner.ports.DriverPowerState == nil {
		return nil
	}
	return runner.ports.DriverPowerState.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (runner *effectRunner) observeDriverBootState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if runner.ports.DriverBootState == nil || host.Spec.Management.BootDriver != redfishDriverName {
		return nil
	}
	return runner.ports.DriverBootState.ObserveBootAndPersist(ctx, driverName, target, host, invocation)
}
