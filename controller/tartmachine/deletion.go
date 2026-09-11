package tartmachine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubernetesadapter "github.com/walnuts1018/cluster-api-provider-tart/adapter/kubernetes"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	machinedomain "github.com/walnuts1018/cluster-api-provider-tart/domain/machine"
	domainpower "github.com/walnuts1018/cluster-api-provider-tart/domain/power"
	machineusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/machine"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func (r *TartMachineReconciler) reconcileDeletion(ctx context.Context, machine *infrav1alpha1.TartMachine) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(machine, tartMachineFinalizer) {
		return ctrl.Result{}, nil
	}
	deletionReady, deletionErr := r.capiDeletionDrainComplete(ctx, machine)
	if deletionErr != nil {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The corresponding CAPI Machine deletion state could not be observed; shutdown and Host release remain blocked.", shutdownConfirmationRequeue)
	}
	if !deletionReady {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The CAPI Machine has not completed drain and volume detach; shutdown and Host release remain blocked.", shutdownConfirmationRequeue)
	}

	selected, err := r.findClaimedHost(ctx, machine)
	if err != nil {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host could not be unambiguously observed; the Machine finalizer remains until shutdown is confirmed.", shutdownConfirmationRequeue)
	}
	if selected == nil {
		original := machine.DeepCopy()
		controllerutil.RemoveFinalizer(machine, tartMachineFinalizer)
		if err := r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	configuration, configurationErr := BootstrapConfiguration(ctx, r.Client, machine)
	if configurationErr != nil && !errors.Is(configurationErr, ErrBootstrapDataUnavailable) {
		return ctrl.Result{}, configurationErr
	}
	if !machineusecase.HasShutdownRequest(machine) {
		requested, requestErr := r.requestHostShutdown(ctx, selected, configuration)
		if requestErr != nil {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host could not be shut down safely; the Machine finalizer remains.", shutdownConfirmationRequeue)
		}
		if !requested {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host is not reachable through a verified Talos API; shutdown has not been confirmed.", shutdownConfirmationRequeue)
		}
		return r.reportAndRequeue(ctx, machine, machinedomain.ShutdownRequestedReason, "Talos shutdown was requested; the Host claim remains until API unreachability is observed.", shutdownConfirmationRequeue)
	}

	if !machineusecase.ShutdownRequestSettled(machine, shutdownConfirmationDelay) {
		return r.reportAndRequeue(ctx, machine, machinedomain.ShutdownRequestedReason, "The Host API is unreachable after shutdown request; waiting for the confirmation interval before retention.", shutdownConfirmationRequeue)
	}

	stopped, observationErr := r.observeHostStopped(ctx, selected, machine, configuration)
	if observationErr != nil {
		if isShutdownConfirmationRequired(selected.Spec.Power.Backend) && !shutdownConfirmed(selected, machine) {
			return r.reportAndRequeue(ctx, machine, machinedomain.ShutdownRequestedReason, "The WakeOnLAN/Manual power backend cannot independently observe power-off state; explicit shutdown confirmation is required after the shutdown request.", shutdownConfirmationRequeue)
		}
		if errors.Is(observationErr, ErrShutdownStateUnverifiable) && isShutdownConfirmationRequired(selected.Spec.Power.Backend) {
			return r.reportAndRequeue(ctx, machine, machinedomain.ShutdownRequestedReason, "The WakeOnLAN/Manual power backend cannot independently observe power-off state; explicit shutdown confirmation is required after the shutdown request.", shutdownConfirmationRequeue)
		}
		return r.reportAndRequeue(ctx, machine, machinedomain.ShutdownRequestedReason, "The allocated Host stop state could not be verified after the shutdown request; the Machine finalizer remains.", shutdownConfirmationRequeue)
	}
	if !stopped {
		if isShutdownConfirmationRequired(selected.Spec.Power.Backend) && !shutdownConfirmed(selected, machine) {
			return r.reportAndRequeue(ctx, machine, machinedomain.ShutdownRequestedReason, "The WakeOnLAN/Manual power backend cannot independently observe power-off state; explicit shutdown confirmation is required after the shutdown request.", shutdownConfirmationRequeue)
		}
		return r.reportAndRequeue(ctx, machine, machinedomain.ShutdownRequestedReason, "The allocated Host still responds to a Talos API after the shutdown request; the Host claim remains.", shutdownConfirmationRequeue)
	}
	consumer := corev1.ObjectReference{
		APIVersion: infrav1alpha1.GroupVersion.String(),
		Kind:       controller.TartMachineKind,
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
	previous, prevErr := r.previousConsumerRef(ctx, machine, consumer)
	if prevErr != nil {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The previous consumer provenance could not be resolved; Host release remains blocked until ClusterID is observable.", shutdownConfirmationRequeue)
	}
	if err := kubernetesadapter.NewTartHostRepository(r.Client).RetainHost(ctx, selected, consumer, previous); err != nil {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The Host retention record could not be written atomically; the Machine finalizer remains.", shutdownConfirmationRequeue)
	}

	original := machine.DeepCopy()
	controllerutil.RemoveFinalizer(machine, tartMachineFinalizer)
	if err := r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// capiDeletionDrainCompleteはprovider resourceの削除前にCAPI Machine controllerがdrainとvolume detachを完了したことを確認する。
// pre-terminate hookがあるcontrol planeでは、そのhook解除後にCAPIがinfra削除段階へ進んだことも同時に確認できる。
func (r *TartMachineReconciler) capiDeletionDrainComplete(ctx context.Context, machine *infrav1alpha1.TartMachine) (bool, error) {
	capiMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return false, err
	}
	return machineusecase.DeletionDrainComplete(capiMachine), nil
}

func (r *TartMachineReconciler) findClaimedHost(ctx context.Context, machine *infrav1alpha1.TartMachine) (*infrav1alpha1.TartHost, error) {
	allHosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, allHosts); err != nil {
		return nil, err
	}

	var statusHost *infrav1alpha1.TartHost
	if machine.Status.HostRef != nil {
		for index := range allHosts.Items {
			candidate := &allHosts.Items[index]
			if candidate.Name == machine.Status.HostRef.Name {
				statusHost = candidate
				break
			}
		}
		if statusHost == nil {
			return nil, apierrors.NewNotFound(schema.GroupResource{Group: infrav1alpha1.GroupVersion.Group, Resource: "tarthosts"}, machine.Status.HostRef.Name)
		}
		if statusHost.Spec.ConsumerRef == nil || statusHost.Spec.ConsumerRef.UID != machine.UID {
			if statusHost.Spec.PreviousConsumerRef != nil && statusHost.Spec.PreviousConsumerRef.UID == machine.UID {
				return nil, nil
			}
			return nil, errMachineHostBindingLost
		}
	}

	claimed, err := machineusecase.FindClaimedHost(allHosts.Items, string(machine.UID))
	if err != nil {
		return nil, err
	}
	if claimed != nil {
		return claimed, nil
	}
	if statusHost != nil && statusHost.Spec.ConsumerRef != nil && statusHost.Spec.ConsumerRef.UID == machine.UID {
		return statusHost, nil
	}
	return nil, nil
}

var errMachineHostBindingLost = errors.New("machine host binding was lost before deletion completed")

// hasIndependentPowerControlは、Talos APIのgraceful shutdownに頼らず、out-of-band
// management channel(Redfish/IntelManageability)経由でHostの電源を直接操作し、
// かつ結果を独立して観測できるbackendかを返す。
func hasIndependentPowerControl(backend infrav1alpha1.PowerBackend) bool {
	return backend == infrav1alpha1.PowerBackendRedfish || backend == infrav1alpha1.PowerBackendIntelManageability
}

func (r *TartMachineReconciler) requestHostShutdown(ctx context.Context, selected *infrav1alpha1.TartHost, configuration []byte) (bool, error) {
	endpoint := controller.HostTalosEndpoint(selected)
	if endpoint == "" {
		return false, nil
	}
	if len(bytes.TrimSpace(configuration)) > 0 {
		connectionContext, cancel := context.WithTimeout(ctx, talosReconcileTimeout)
		authenticated, err := talos.DialAuthenticatedFromConfiguration(connectionContext, endpoint, configuration)
		cancel()
		if err == nil {
			shutdownContext, shutdownCancel := context.WithTimeout(ctx, talosReconcileTimeout)
			shutdownErr := authenticated.Shutdown(shutdownContext)
			shutdownCancel()
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			if shutdownErr == nil {
				return true, nil
			}
		}
	}

	dialCtx, cancel := context.WithTimeout(ctx, maintenanceDialTimeout)
	maintenance, err := talos.DialMaintenance(dialCtx, endpoint)
	cancel()
	if err != nil {
		return false, nil //nolint:nilerr // maintenance mode may not be reachable until the Host finishes shutting down.
	}
	inventoryCtx, cancel := context.WithTimeout(ctx, maintenanceObserveTimeout)
	identity, identityErr := maintenance.Inventory(inventoryCtx)
	cancel()
	if identityErr != nil {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return false, nil
	}
	if !identity.HasMAC(selected.Spec.MACAddress) {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return false, controller.ErrHostIdentityMismatch
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, maintenanceActionTimeout)
	shutdownErr := maintenance.Shutdown(shutdownCtx)
	cancel()
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	if shutdownErr != nil {
		// Talosのmaintenance mode APIはbootstrap前の最小限のRPCしか提供せず、Shutdownは
		// 実装されていない(常にUnimplementedを返す)。graceful shutdownをTalos側へ要求できない
		// ため、独立してpower stateを観測・確認できるbackendに限りout-of-band power offへfallbackする。
		// WakeOnLAN/Manualは独立した観測手段がなくfallbackが安全側に倒れないため対象外とする。
		if hasIndependentPowerControl(selected.Spec.Power.Backend) {
			if powerOffErr := power.PowerOffHost(ctx, r.Client, r.ManagementNamespace, selected); powerOffErr == nil {
				return true, nil
			}
		}
		return false, shutdownErr
	}
	return true, nil
}

func isShutdownConfirmationRequired(backend infrav1alpha1.PowerBackend) bool {
	return backend == infrav1alpha1.PowerBackendWakeOnLAN || backend == infrav1alpha1.PowerBackendManual
}

func shutdownConfirmed(host *infrav1alpha1.TartHost, machine *infrav1alpha1.TartMachine) bool {
	confirmation := host.Spec.ShutdownConfirmation
	if confirmation == nil {
		return false
	}
	if strings.TrimSpace(string(confirmation.ConsumerUID)) == "" || confirmation.ConsumerUID != machine.UID {
		return false
	}
	if confirmation.HostID.IsZero() || confirmation.HostID != host.Spec.HostID {
		return false
	}
	// inventoryにBootIDが存在する場合は、confirmationでも必須とする。stale confirmationで再起動後のHostを誤って停止済み扱いしないため。
	if inventory := host.Status.Inventory; inventory != nil && strings.TrimSpace(inventory.BootID) != "" {
		if strings.TrimSpace(confirmation.BootID) == "" || confirmation.BootID != inventory.BootID {
			return false
		}
	}
	return true
}

func (r *TartMachineReconciler) observeHostStopped(ctx context.Context, selected *infrav1alpha1.TartHost, machine *infrav1alpha1.TartMachine, configuration []byte) (bool, error) {
	switch selected.Spec.Power.Backend {
	case infrav1alpha1.PowerBackendRedfish:
		state, err := power.RedfishPowerState(ctx, r.Client, r.ManagementNamespace, selected)
		if err != nil {
			return false, err
		}
		return state == domainpower.PowerStateOff, nil
	case infrav1alpha1.PowerBackendIntelManageability:
		state, err := power.IntelManageabilityPowerState(ctx, r.Client, r.ManagementNamespace, selected)
		if err != nil {
			return false, err
		}
		return state == domainpower.PowerStateOff, nil
	case infrav1alpha1.PowerBackendWakeOnLAN, infrav1alpha1.PowerBackendManual:
		// WoL/Manualは独立したpower-state observerを持たないため、以下のTalos到達性による代替判定へ進む。
	}
	endpoint := controller.HostTalosEndpoint(selected)
	if endpoint == "" {
		return false, controller.ErrHostEndpointUnavailable
	}
	// WoL/Manualでは、まずTalosが現在動いているという正の証拠を優先する。confirmationが存在しても、Talosが応答している間は停止済み扱いにしない。
	var authenticatedErr error
	if len(bytes.TrimSpace(configuration)) > 0 {
		connectionContext, cancel := context.WithTimeout(ctx, talosReconcileTimeout)
		authenticated, err := talos.DialAuthenticatedFromConfiguration(connectionContext, endpoint, configuration)
		cancel()
		if err == nil {
			versionContext, versionCancel := context.WithTimeout(ctx, talosReconcileTimeout)
			_, versionErr := authenticated.Version(versionContext)
			versionCancel()
			if closeErr := authenticated.Close(); closeErr != nil {
				ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
			}
			if versionErr == nil {
				return false, nil
			}
			authenticatedErr = versionErr
		}
		if err != nil {
			authenticatedErr = err
		}
	}
	dialCtx, cancel := context.WithTimeout(ctx, maintenanceDialTimeout)
	maintenance, err := talos.DialMaintenance(dialCtx, endpoint)
	cancel()
	maintenanceErr := err
	if err == nil {
		inventoryCtx, cancel := context.WithTimeout(ctx, maintenanceObserveTimeout)
		identity, identityErr := maintenance.Inventory(inventoryCtx)
		cancel()
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		if identityErr == nil {
			if !identity.HasMAC(selected.Spec.MACAddress) {
				return false, controller.ErrHostIdentityMismatch
			}
			// maintenanceが正しいidentityで応答している間は、confirmationより優先して起動中として扱う。
			return false, nil
		}
		if authenticatedErr == nil {
			authenticatedErr = identityErr
		}
		maintenanceErr = identityErr
	}
	// ここまでで、どのTalos APIも正の証拠として応答していない。独立observerがないため、operatorの明示的なconfirmationが必要。
	if isShutdownConfirmationRequired(selected.Spec.Power.Backend) && shutdownConfirmed(selected, machine) {
		return true, nil
	}
	if authenticatedErr != nil {
		return false, fmt.Errorf("%w: maintenance API unreachable is not proof of power off (authenticated error: %v): %w", ErrShutdownStateUnverifiable, authenticatedErr, maintenanceErr)
	}
	return false, fmt.Errorf("%w: maintenance API unreachable is not proof of power off: %w", ErrShutdownStateUnverifiable, maintenanceErr)
}

func (r *TartMachineReconciler) previousConsumerRef(ctx context.Context, machine *infrav1alpha1.TartMachine, consumer corev1.ObjectReference) (infrav1alpha1.PreviousConsumerRef, error) {
	previous := infrav1alpha1.PreviousConsumerRef{
		Namespace: consumer.Namespace,
		Name:      consumer.Name,
		UID:       consumer.UID,
	}
	// CAPI Machineがまだ存在するならClusterIDは解決できるはずであり、ErrCAPIMachineUnavailableを
	// 含むあらゆる失敗を一時的な観測失敗として扱い、releaseをブロックする。
	capiMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return previous, fmt.Errorf("resolve previous consumer ClusterID: %w", err)
	}
	var cluster clusterv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: capiMachine.Namespace, Name: capiMachine.Spec.ClusterName}, &cluster); err != nil {
		return previous, fmt.Errorf("resolve previous consumer ClusterID: %w", err)
	}
	if ref := cluster.Spec.InfrastructureRef; ref.APIGroup == infrav1alpha1.GroupVersion.Group && ref.Kind == controller.TartClusterKind && ref.Name != "" {
		var tartCluster infrav1alpha1.TartCluster
		if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, &tartCluster); err != nil {
			return previous, fmt.Errorf("resolve previous consumer ClusterID: %w", err)
		}
		if tartCluster.Spec.ClusterID.IsZero() {
			return previous, fmt.Errorf("resolve previous consumer ClusterID: TartCluster ClusterID is empty")
		}
		previous.ClusterID = tartCluster.Spec.ClusterID
	}
	if previous.ClusterID.IsZero() {
		return previous, fmt.Errorf("resolve previous consumer ClusterID: ClusterID is empty")
	}
	return previous, nil
}
