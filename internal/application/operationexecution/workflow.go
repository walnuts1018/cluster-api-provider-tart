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
	"time"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	inplaceupdatedomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/inplaceupdate"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/slot"
)

const (
	// Deadlineは外部reportなしでも進むため、activeなOperationは定期観測する。
	DeadlineRequeueInterval = 1 * time.Minute
	redfishDriverName       = "redfish"
)

// PowerOnService はOperationのPreparingBootフェーズで電源投入を発火する。
type PowerOnService interface {
	PowerOn(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		operationdomain.ID,
		applicationdriver.Invocation,
	) error
}

// BootPreparationService はPowerOn前に利用するboot transportを準備する。
type BootPreparationService interface {
	PrepareBoot(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		operationdomain.ID,
		*driverdomain.BootTarget,
		applicationdriver.Invocation,
	) (driverdomain.BootTarget, error)
}

// HostPhaseService はTartHostのPhaseをOperation結果に応じて更新する。
type HostPhaseService interface {
	MarkHostProvisioning(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostUpdating(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostProvisioned(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostRecoveryRequired(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostAvailable(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostCleaningForDeletion(
		ctx context.Context,
		host *infrastructurev1beta1.TartHost,
		deletionPolicy infrastructurev1beta1.DeletionPolicy,
	) error
	MarkHostRetained(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostDetached(ctx context.Context, host *infrastructurev1beta1.TartHost) error
}

// DriverTargetBuilder はTartHostからdriver呼び出し対象を構築する。
type DriverTargetBuilder interface {
	Build(context.Context, *infrastructurev1beta1.TartHost) (driverdomain.HostTarget, error)
}

// DriverCapabilityObserver はHostごとのdriver capabilityを観測しStatusへ反映する。
type DriverCapabilityObserver interface {
	ObserveAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}

// DriverPowerStateObserver はHostごとのdriver power stateを観測しStatusへ反映する。
type DriverPowerStateObserver interface {
	ObserveAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}

// DriverBootStateObserver はHostごとのdriver boot stateを観測しStatusへ反映する。
type DriverBootStateObserver interface {
	ObserveBootAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}

type Result struct {
	RequeueAfter time.Duration
}

// Workflow は純粋なOperation Process Managerと副作用Stepを接続する。
type Workflow struct {
	client.Client
	PowerOn            PowerOnService
	PrepareBoot        BootPreparationService
	HostPhase          HostPhaseService
	Targets            DriverTargetBuilder
	DriverCapabilities DriverCapabilityObserver
	DriverPowerState   DriverPowerStateObserver
	DriverBootState    DriverBootStateObserver
	now                func() time.Time
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
	return &Workflow{
		Client:             k8sClient,
		PowerOn:            powerOn,
		PrepareBoot:        prepareBoot,
		HostPhase:          hostPhase,
		Targets:            targets,
		DriverCapabilities: driverCapabilities,
		DriverPowerState:   driverPowerState,
		DriverBootState:    driverBootState,
		now:                time.Now,
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (Result, error) {
	decision, err := workflow.decide(ctx, operation)
	if err != nil {
		return Result{}, err
	}
	return workflow.executeCommand(ctx, operation, decision.Command)
}

func (workflow *Workflow) decide(
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
	cleaningPolicy, err := workflow.operationCleaningPolicy(ctx, operation)
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
		Now: workflow.now(),
	})
}

func (workflow *Workflow) executeCommand(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	command operationdomain.Command,
) (Result, error) {
	log := logf.FromContext(ctx)

	switch selected := command.(type) {
	case operationdomain.CommandInitializePending:
		return Result{}, workflow.transitionPhase(ctx, operation, apiOperationPhase(selected.Target))
	case operationdomain.CommandPrepareBoot:
		return Result{}, workflow.prepareOperationBoot(ctx, operation, selected.Host)
	case operationdomain.CommandObserveActive:
		if err := workflow.observeActiveOperationDriverState(ctx, operation); err != nil {
			return Result{}, err
		}
		return Result{RequeueAfter: DeadlineRequeueInterval}, nil
	case operationdomain.CommandAwaitMachineHealth:
		return Result{RequeueAfter: DeadlineRequeueInterval}, nil
	case operationdomain.CommandCompleteWipeAll:
		return Result{}, workflow.completeOperationWithHostCommand(ctx, operation, selected.Host, selected.Target)
	case operationdomain.CommandCompleteCleaning:
		return Result{}, workflow.completeOperationWithHostCommand(ctx, operation, selected.Host, selected.Target)
	case operationdomain.CommandHandleTerminal:
		return Result{}, workflow.applyHostCommand(ctx, operation, selected.Host)
	case operationdomain.CommandFailDeadlineExceeded:
		log.Info("TartHostOperation deadline exceeded",
			"operation", client.ObjectKeyFromObject(operation).String(),
			"deadline", operation.Spec.Deadline.Time,
			"phase", operation.Status.Phase,
		)
		return Result{}, workflow.applyDeadlineOutcome(ctx, operation, selected.Outcome)
	case operationdomain.CommandIgnore:
		log.V(4).Info("TartHostOperation in unhandled phase, skipping",
			"operation", client.ObjectKeyFromObject(operation).String(),
			"phase", operation.Status.Phase,
		)
		return Result{}, nil
	default:
		return Result{}, fmt.Errorf("unknown TartHostOperation workflow command %T", selected)
	}
}

func (workflow *Workflow) operationCleaningPolicy(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (operationdomain.CleaningPolicy, error) {
	switch operation.Spec.Type {
	case infrastructurev1beta1.OperationTypeClean, infrastructurev1beta1.OperationTypeWipeAll:
		policy, err := workflow.cleaningPolicy(ctx, operation)
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

func (workflow *Workflow) prepareOperationBoot(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	hostCommand operationdomain.HostCommand,
) error {
	log := logf.FromContext(ctx)

	host, err := workflow.getHost(ctx, operation)
	if err != nil {
		return err
	}
	operationID, err := operationdomain.ParseID(operation.Spec.OperationID)
	if err != nil {
		return fmt.Errorf("parse operation ID: %w", err)
	}
	target, err := workflow.driverTarget(ctx, host)
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
	if err := workflow.observeDriverCapabilities(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost driver capabilities",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost driver capabilities: %w", err)
	}
	if err := workflow.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost power state",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := workflow.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost boot state",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", bootDriverName,
		)
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	if err := workflow.prepareBoot(ctx, host, bootDriverName, target, operationID, invocation); err != nil {
		log.Error(err, "Failed to prepare TartHost boot transport",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", bootDriverName,
		)
		return fmt.Errorf("prepare TartHost boot transport: %w", err)
	}
	if err := workflow.PowerOn.PowerOn(ctx, powerDriverName, target, operationID, invocation); err != nil {
		log.Error(err, "Failed to power on TartHost for Operation",
			"operation", client.ObjectKeyFromObject(operation).String(),
			"host", operation.Spec.HostRef.Name,
			"driver", host.Spec.Management.PowerDriver,
		)
		return fmt.Errorf("power on TartHost: %w", err)
	}
	if err := workflow.applyHostCommandToHost(ctx, operation, host, hostCommand); err != nil {
		log.Error(err, "Failed to mark TartHost for Operation",
			"host", client.ObjectKeyFromObject(host).String(),
		)
		return err
	}
	return workflow.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhasePreparingBoot)
}

func (workflow *Workflow) completeOperationWithHostCommand(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	hostCommand operationdomain.HostCommand,
	target operationdomain.Phase,
) error {
	if err := workflow.applyHostCommand(ctx, operation, hostCommand); err != nil {
		return err
	}
	return workflow.transitionPhase(ctx, operation, apiOperationPhase(target))
}

func (workflow *Workflow) applyHostCommand(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	command operationdomain.HostCommand,
) error {
	if _, ok := command.(operationdomain.HostNoop); ok {
		return nil
	}
	host, err := workflow.getHost(ctx, operation)
	if err != nil {
		return err
	}
	return workflow.applyHostCommandToHost(ctx, operation, host, command)
}

func (workflow *Workflow) applyHostCommandToHost(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	host *infrastructurev1beta1.TartHost,
	command operationdomain.HostCommand,
) error {
	switch selected := command.(type) {
	case operationdomain.HostNoop:
		return nil
	case operationdomain.HostMarkProvisioning:
		return workflow.HostPhase.MarkHostProvisioning(ctx, host)
	case operationdomain.HostMarkUpdating:
		return workflow.HostPhase.MarkHostUpdating(ctx, host)
	case operationdomain.HostMarkCleaning:
		return workflow.HostPhase.MarkHostCleaningForDeletion(ctx, host, apiCleaningPolicy(selected.Policy))
	case operationdomain.HostMarkAvailable:
		return workflow.HostPhase.MarkHostAvailable(ctx, host)
	case operationdomain.HostMarkRetained:
		return workflow.HostPhase.MarkHostRetained(ctx, host)
	case operationdomain.HostMarkDetached:
		return workflow.HostPhase.MarkHostDetached(ctx, host)
	case operationdomain.HostMarkProvisioned:
		return workflow.HostPhase.MarkHostProvisioned(ctx, host)
	case operationdomain.HostMarkRecoveryRequired:
		return workflow.HostPhase.MarkHostRecoveryRequired(ctx, host)
	default:
		return fmt.Errorf("unknown TartHostOperation host command %T for %s", selected, client.ObjectKeyFromObject(operation).String())
	}
}

func (workflow *Workflow) applyDeadlineOutcome(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	outcome operationdomain.DeadlineOutcome,
) error {
	switch selected := outcome.(type) {
	case operationdomain.DeadlineMarkFailed:
		if selected.WithUpdateFailure {
			return workflow.transitionUpdateFailurePhase(ctx, operation, apiOperationPhase(selected.FailedPhase), infrastructurev1beta1.TartHostOperationPhaseFailed)
		}
		return workflow.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseFailed)
	case operationdomain.DeadlineRecordBootFailure:
		return workflow.handleBootTrialDeadlineExceeded(ctx, operation)
	case operationdomain.DeadlineTransitionFailure:
		return workflow.transitionUpdateFailurePhase(ctx, operation, apiOperationPhase(selected.FailedPhase), apiOperationPhase(selected.Target))
	default:
		return fmt.Errorf("unknown TartHostOperation deadline outcome %T", selected)
	}
}

func (workflow *Workflow) prepareBoot(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
	invocation applicationdriver.Invocation,
) error {
	if workflow.PrepareBoot == nil || host.Spec.Management.BootDriver != redfishDriverName {
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
	_, err = workflow.PrepareBoot.PrepareBoot(ctx, driverName, target, operationID, targetOverride, invocation)
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

func (workflow *Workflow) driverTarget(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
) (driverdomain.HostTarget, error) {
	if workflow.Targets != nil {
		return workflow.Targets.Build(ctx, host)
	}
	bootMAC, err := driverdomain.ParseMACAddress(host.Spec.Identifiers.BootMACAddress)
	if err != nil {
		return driverdomain.HostTarget{}, fmt.Errorf("parse TartHost boot MAC address: %w", err)
	}
	return driverdomain.NewHostTarget(bootMAC), nil
}

func (workflow *Workflow) observeDriverCapabilities(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if workflow.DriverCapabilities == nil {
		return nil
	}
	return workflow.DriverCapabilities.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (workflow *Workflow) observeDriverPowerState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if workflow.DriverPowerState == nil {
		return nil
	}
	return workflow.DriverPowerState.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (workflow *Workflow) observeDriverBootState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if workflow.DriverBootState == nil || host.Spec.Management.BootDriver != redfishDriverName {
		return nil
	}
	return workflow.DriverBootState.ObserveBootAndPersist(ctx, driverName, target, host, invocation)
}

func (workflow *Workflow) observeActiveOperationDriverState(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if workflow.DriverBootState == nil && workflow.DriverPowerState == nil {
		return nil
	}
	host, err := workflow.getHost(ctx, operation)
	if err != nil {
		return err
	}
	target, err := workflow.driverTarget(ctx, host)
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
	if err := workflow.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := workflow.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	return nil
}

func (workflow *Workflow) cleaningPolicy(
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
	if err := workflow.Get(ctx, client.ObjectKey{
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

func (workflow *Workflow) getHost(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHost, error) {
	host := &infrastructurev1beta1.TartHost{}
	if err := workflow.Get(ctx, client.ObjectKey{
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

func (workflow *Workflow) transitionPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := workflow.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return workflow.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (workflow *Workflow) handleBootTrialDeadlineExceeded(
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
		if err := workflow.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
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
		return workflow.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (workflow *Workflow) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := workflow.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		appupdate.UpdateFailureCondition(&current.Status, current.Generation, failedPhase, target)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return workflow.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}
