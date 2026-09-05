package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/boot"
	"github.com/walnuts1018/cluster-api-provider-tart/bootstrap"
	"github.com/walnuts1018/cluster-api-provider-tart/host"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	tartMachineFinalizer        = "tart.cluster.x-k8s.io/machine-lifecycle"
	shutdownConfirmationRequeue = 30 * time.Second
	talosReconcileTimeout       = 20 * time.Second
	talosRequeue                = 30 * time.Second
)

var (
	errBootstrapDataUnavailable = errors.New("bootstrap data is unavailable")
	errCAPIProviderIDMismatch   = errors.New("CAPI Machine provider ID does not match TartHost")
)

// TartMachineReconcilerはHost claim、Talosの初回configuration apply、認証済みAPIの
// 起動確認を担当する。初回provisioning後のmutableな変更はUpdate Extensionへ委譲する。
type TartMachineReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=,resources=secrets,verbs=get;list;watch

func (r *TartMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var machine infrav1alpha1.TartMachine
	if err := r.Get(ctx, req.NamespacedName, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&machine) {
		return ctrl.Result{}, nil
	}

	if !machine.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, &machine)
	}
	if !controllerutil.ContainsFinalizer(&machine, tartMachineFinalizer) {
		original := machine.DeepCopy()
		controllerutil.AddFinalizer(&machine, tartMachineFinalizer)
		if err := r.Patch(ctx, &machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	allHosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, allHosts); err != nil {
		return ctrl.Result{}, err
	}
	if host.HasIdentityConflictForAny(allHosts.Items) {
		return r.report(ctx, &machine, infrav1alpha1.ReasonIdentityConflict, "Stable Host identity is duplicated; allocation is stopped until the conflict is resolved.")
	}

	selected, err := r.observedOrSelectedHost(ctx, &machine, allHosts.Items)
	if err != nil {
		if errors.Is(err, host.ErrNoEligibleHost) {
			return r.reportAndRequeue(ctx, &machine, infrav1alpha1.ReasonNoEligibleHost, "No eligible fresh TartHost is available for this Machine.", 30*time.Second)
		}
		if apierrors.IsNotFound(err) {
			return r.reportAndRequeue(ctx, &machine, infrav1alpha1.ReasonHostNotFound, "The referenced TartHost was not found.", 30*time.Second)
		}
		return ctrl.Result{}, err
	}
	if selected == nil {
		return r.report(ctx, &machine, infrav1alpha1.ReasonHostMismatch, "The observed Host binding is unavailable.")
	}

	if selected.Spec.HostID == "" {
		return r.report(ctx, &machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost has no persistent identity yet.")
	}
	providerID, err := host.ProviderID(selected.Spec.HostID)
	if err != nil {
		return r.report(ctx, &machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost identity is invalid.")
	}
	if machine.Spec.ProviderID != "" && machine.Spec.ProviderID != providerID {
		return r.report(ctx, &machine, infrav1alpha1.ReasonHostMismatch, "The existing ProviderID does not match the allocated TartHost identity.")
	}

	consumer := corev1.ObjectReference{
		APIVersion: infrav1alpha1.GroupVersion.String(),
		Kind:       tartMachineKind,
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
	if err := host.Claim(ctx, r.Client, selected, consumer); err != nil {
		if errors.Is(err, host.ErrClaimConflict) {
			return r.reportAndRequeue(ctx, &machine, infrav1alpha1.ReasonHostClaimConflict, "The selected TartHost was claimed concurrently; allocation will be retried against current state.", 2*time.Second)
		}
		return ctrl.Result{}, err
	}

	if machine.Spec.ProviderID == "" {
		original := machine.DeepCopy()
		machine.Spec.ProviderID = providerID
		if err := r.Patch(ctx, &machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.syncCAPIProviderID(ctx, &machine, providerID); err != nil {
		if errors.Is(err, errCAPIProviderIDMismatch) {
			return r.report(ctx, &machine, infrav1alpha1.ReasonHostMismatch, "The CAPI Machine ProviderID does not match the allocated TartHost identity.")
		}
		return ctrl.Result{}, err
	}

	statusOriginal := machine.DeepCopy()
	machine.Status.HostRef = &corev1.LocalObjectReference{Name: selected.Name}
	if err := r.Status().Patch(ctx, &machine, client.MergeFrom(statusOriginal)); err != nil {
		return ctrl.Result{}, err
	}
	configuration, err := r.bootstrapConfiguration(ctx, &machine)
	if err != nil {
		if errors.Is(err, errBootstrapDataUnavailable) {
			return r.reportTalosStatus(ctx, &machine,
				metav1.ConditionFalse, "BootstrapDataUnavailable", "Talos provisioning is waiting for bootstrap data.",
				metav1.ConditionFalse, "BootstrapDataUnavailable", "Talos provisioning is waiting for bootstrap data.",
				"BootstrapDataUnavailable", "Talos version cannot be verified before provisioning.",
				"BootstrapDataUnavailable", "The immutable Bootstrap Secret is not available yet.",
				talosRequeue)
		}
		return ctrl.Result{}, err
	}

	return r.reconcileTalos(ctx, &machine, selected, configuration)
}

func (r *TartMachineReconciler) syncCAPIProviderID(ctx context.Context, machine *infrav1alpha1.TartMachine, providerID string) error {
	var owner *metav1.OwnerReference
	for index := range machine.OwnerReferences {
		candidate := &machine.OwnerReferences[index]
		if candidate.Kind == capiMachineKind && candidate.APIVersion == clusterv1.GroupVersion.String() {
			owner = candidate
			break
		}
	}
	if owner == nil {
		return nil
	}

	clusterMachine := &clusterv1.Machine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: owner.Name}, clusterMachine); err != nil {
		return err
	}
	if owner.UID != "" && clusterMachine.UID != owner.UID {
		return errCAPIProviderIDMismatch
	}
	if clusterMachine.Spec.ProviderID != "" {
		if clusterMachine.Spec.ProviderID != providerID {
			return errCAPIProviderIDMismatch
		}
		return nil
	}
	original := clusterMachine.DeepCopy()
	clusterMachine.Spec.ProviderID = providerID
	return r.Patch(ctx, clusterMachine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func (r *TartMachineReconciler) bootstrapConfiguration(ctx context.Context, machine *infrav1alpha1.TartMachine) ([]byte, error) {
	var owner *metav1.OwnerReference
	for index := range machine.OwnerReferences {
		candidate := &machine.OwnerReferences[index]
		if candidate.Kind == capiMachineKind && candidate.APIVersion == clusterv1.GroupVersion.String() {
			owner = candidate
			break
		}
	}
	if owner == nil {
		return nil, errBootstrapDataUnavailable
	}

	clusterMachine := &clusterv1.Machine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: owner.Name}, clusterMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errBootstrapDataUnavailable
		}
		return nil, err
	}
	if owner.UID != "" && clusterMachine.UID != owner.UID {
		return nil, errBootstrapDataUnavailable
	}
	ref := clusterMachine.Spec.Bootstrap.ConfigRef
	if ref.Name == "" || ref.Kind != tartBootstrapConfigKind || ref.APIGroup != bootstrapv1alpha1.GroupVersion.Group {
		return nil, errBootstrapDataUnavailable
	}

	config := &bootstrapv1alpha1.TartBootstrapConfig{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, config); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errBootstrapDataUnavailable
		}
		return nil, err
	}
	if strings.TrimSpace(config.Status.DataSecretName) == "" {
		return nil, errBootstrapDataUnavailable
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: config.Status.DataSecretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errBootstrapDataUnavailable
		}
		return nil, err
	}
	clusterName := config.Labels[bootstrap.ClusterNameLabel]
	if !bootstrap.IsContractSecret(secret, clusterName, config.UID) {
		return nil, errBootstrapDataUnavailable
	}
	configuration, ok := secret.Data[bootstrap.BootstrapSecretKey]
	if !ok || len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errBootstrapDataUnavailable
	}
	return bytes.Clone(configuration), nil
}

func (r *TartMachineReconciler) reconcileTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, configuration []byte) (ctrl.Result, error) {
	endpoint := hostTalosEndpoint(selected)
	if endpoint == "" {
		if err := powerOnHost(ctx, selected); err != nil {
			return r.reportTalosStatus(ctx, machine,
				metav1.ConditionFalse, "PowerUnavailable", "Talos maintenance endpoint is unavailable and the Host could not be powered on.",
				metav1.ConditionFalse, "PowerUnavailable", "Talos provisioning is waiting for the Host power capability.",
				"PowerUnavailable", "Talos version cannot be verified before the Host is reachable.",
				"PowerUnavailable", "The Host does not expose a reachable Talos endpoint.",
				talosRequeue)
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "EndpointUnavailable", "The Host was powered on; waiting for a Talos maintenance endpoint.",
			metav1.ConditionFalse, "EndpointUnavailable", "Talos installation has not started because the Host endpoint is not observed.",
			"EndpointUnavailable", "Talos version cannot be verified before the Host is reachable.",
			"EndpointUnavailable", "The Host Talos endpoint is not available yet.",
			talosRequeue)
	}

	connectionContext, cancel := context.WithTimeout(ctx, talosReconcileTimeout)
	authenticated, authErr := talos.DialAuthenticatedFromConfiguration(connectionContext, endpoint, configuration)
	cancel()
	if authErr == nil {
		versionContext, versionCancel := context.WithTimeout(ctx, talosReconcileTimeout)
		version, versionErr := authenticated.Version(versionContext)
		versionCancel()
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		if versionErr != nil {
			return r.reportTalosStatus(ctx, machine,
				metav1.ConditionFalse, "TalosUnreachable", "The authenticated Talos API could not be queried.",
				metav1.ConditionFalse, "TalosUnreachable", "Talos provisioning has not been confirmed.",
				"TalosUnreachable", "The desired Talos version cannot be verified.",
				"TalosUnreachable", "The authenticated Talos API is not reachable.",
				talosRequeue)
		}

		if machine.Spec.Image.Version != "" && version.Tag == machine.Spec.Image.Version {
			return r.reportTalosStatusWithVersion(ctx, machine, version.Tag,
				metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
				metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
				metav1.ConditionTrue, "UpToDate", "The observed Talos version matches the desired version.",
				metav1.ConditionTrue, "Ready", "The Host is running the desired Talos version.",
				0)
		}
		return r.reportTalosStatusWithVersion(ctx, machine, version.Tag,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, "VersionMismatch", "The observed Talos version does not match the desired version.",
			metav1.ConditionFalse, "VersionMismatch", "The Host is running Talos, but not the desired version.",
			0)
	}

	if isProvisioned(machine) {
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "TalosUnreachable", "The authenticated Talos API is not reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation was previously observed.",
			"TalosUnreachable", "The desired Talos version cannot be verified.",
			"TalosUnreachable", "The provisioned Host is temporarily unreachable.",
			talosRequeue)
	}

	maintenance, maintenanceErr := talos.DialMaintenance(ctx, endpoint)
	if maintenanceErr != nil {
		if powerErr := powerOnHost(ctx, selected); powerErr != nil {
			return r.reportTalosStatus(ctx, machine,
				metav1.ConditionFalse, "MaintenanceUnavailable", "The Talos maintenance API is unavailable and the Host could not be powered on.",
				metav1.ConditionFalse, "MaintenanceUnavailable", "Talos installation is waiting for a reachable maintenance API.",
				"MaintenanceUnavailable", "Talos version cannot be verified before installation.",
				"MaintenanceUnavailable", "The Talos maintenance API is not reachable.",
				talosRequeue)
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "MaintenanceUnavailable", "The Host was powered on; waiting for the Talos maintenance API.",
			metav1.ConditionFalse, "MaintenanceUnavailable", "Talos installation is waiting for a reachable maintenance API.",
			"MaintenanceUnavailable", "Talos version cannot be verified before installation.",
			"MaintenanceUnavailable", "The Talos maintenance API is not reachable yet.",
			talosRequeue)
	}

	identity, identityErr := maintenance.Inventory(ctx)
	if identityErr != nil {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "MaintenanceUnavailable", "The Talos maintenance identity could not be observed.",
			metav1.ConditionFalse, "MaintenanceUnavailable", "Talos installation is waiting for verified Host identity.",
			"MaintenanceUnavailable", "Talos version cannot be verified before installation.",
			"MaintenanceUnavailable", "The Talos maintenance inventory is not available.",
			talosRequeue)
	}
	if !identity.HasMAC(selected.Spec.MACAddress) {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "The Talos endpoint MAC address does not match the claimed Host.",
			metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "Talos configuration apply is stopped until Host identity matches.",
			infrav1alpha1.ReasonIdentityConflict, "Talos version cannot be trusted for a different Host.",
			infrav1alpha1.ReasonIdentityConflict, "The Talos endpoint belongs to a different Host identity.",
			0)
	}

	effectiveConfiguration, err := talos.SetInstallerImage(configuration, machine.Spec.Image.Version, machine.Spec.Image.SchematicID)
	if err != nil {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "ConfigurationInvalid", "The Talos machine configuration could not be prepared for the desired installer image.",
			metav1.ConditionFalse, "ConfigurationInvalid", "Talos installation has not been confirmed.",
			"ConfigurationInvalid", "Talos version cannot be verified before installation.",
			"ConfigurationInvalid", "The desired Talos installer image could not be applied to the machine configuration.",
			talosRequeue)
	}
	if err := maintenance.ApplyConfiguration(ctx, effectiveConfiguration); err != nil {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "ConfigurationApplyFailed", "The complete Talos machine configuration could not be applied.",
			metav1.ConditionFalse, "ConfigurationApplyFailed", "Talos installation has not been confirmed.",
			"ConfigurationApplyFailed", "Talos version cannot be verified before installation.",
			"ConfigurationApplyFailed", "The Talos maintenance API rejected the machine configuration.",
			talosRequeue)
	}
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	return r.reportTalosStatus(ctx, machine,
		metav1.ConditionTrue, "MaintenanceReachable", "The Talos maintenance API accepted the machine configuration.",
		metav1.ConditionFalse, "Provisioning", "Talos is installing the machine configuration and will reboot.",
		"Provisioning", "The authenticated Talos version will be checked after reboot.",
		"Provisioning", "Talos installation is in progress.",
		talosRequeue)
}

func hostTalosEndpoint(host *infrav1alpha1.TartHost) string {
	if endpoint := strings.TrimSpace(host.Spec.TalosAPIAddress); endpoint != "" {
		return endpoint
	}
	for _, addressType := range []clusterv1.MachineAddressType{clusterv1.MachineInternalIP, clusterv1.MachineExternalIP, clusterv1.MachineHostName} {
		for _, address := range host.Status.Addresses {
			if address.Type == addressType && strings.TrimSpace(address.Address) != "" {
				return strings.TrimSpace(address.Address)
			}
		}
	}
	return ""
}

func powerOnHost(ctx context.Context, host *infrav1alpha1.TartHost) error {
	if host.Spec.Power.Backend != infrav1alpha1.PowerBackendWakeOnLAN || host.Spec.Power.WakeOnLAN == nil {
		return fmt.Errorf("host power backend %q cannot power on through the normal path", host.Spec.Power.Backend)
	}
	backend := boot.NewWakeOnLAN(host.Spec.MACAddress, host.Spec.Power.WakeOnLAN.BroadcastAddress)
	return backend.PowerOn(ctx)
}

func isProvisioned(machine *infrav1alpha1.TartMachine) bool {
	return machine.Status.Initialization.Provisioned != nil && *machine.Status.Initialization.Provisioned
}

func (r *TartMachineReconciler) reportTalosStatus(ctx context.Context, machine *infrav1alpha1.TartMachine,
	reachableStatus metav1.ConditionStatus, reachableReason, reachableMessage string,
	provisionedStatus metav1.ConditionStatus, provisionedReason, provisionedMessage string,
	upToDateReason, upToDateMessage, readyReason, readyMessage string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	return r.reportTalosStatusWithVersion(ctx, machine, "", reachableStatus, reachableReason, reachableMessage, provisionedStatus, provisionedReason, provisionedMessage, metav1.ConditionFalse, upToDateReason, upToDateMessage, metav1.ConditionFalse, readyReason, readyMessage, requeueAfter)
}

func (r *TartMachineReconciler) reportTalosStatusWithVersion(ctx context.Context, machine *infrav1alpha1.TartMachine, talosVersion string,
	reachableStatus metav1.ConditionStatus, reachableReason, reachableMessage string,
	provisionedStatus metav1.ConditionStatus, provisionedReason, provisionedMessage string,
	upToDateStatus metav1.ConditionStatus, upToDateReason, upToDateMessage string,
	readyStatus metav1.ConditionStatus, readyReason, readyMessage string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	original := machine.DeepCopy()
	if talosVersion != "" {
		machine.Status.TalosVersion = talosVersion
	}
	if provisionedStatus == metav1.ConditionTrue {
		machine.Status.Initialization.Provisioned = new(true)
	}
	setCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineTalosReachableCondition, reachableStatus, reachableReason, reachableMessage, machine.Generation)
	setCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineProvisionedCondition, provisionedStatus, provisionedReason, provisionedMessage, machine.Generation)
	setCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineTalosUpToDateCondition, upToDateStatus, upToDateReason, upToDateMessage, machine.Generation)
	setCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition, readyStatus, readyReason, readyMessage, machine.Generation)
	machine.Status.ObservedGeneration = machine.Generation
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *TartMachineReconciler) observedOrSelectedHost(ctx context.Context, machine *infrav1alpha1.TartMachine, hosts []infrav1alpha1.TartHost) (*infrav1alpha1.TartHost, error) {
	if machine.Status.HostRef != nil {
		observed := &infrav1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKey{Name: machine.Status.HostRef.Name}, observed); err != nil {
			return nil, err
		}
		if observed.Spec.ConsumerRef == nil || observed.Spec.ConsumerRef.UID != machine.UID {
			return nil, nil
		}
		return observed, nil
	}
	for index := range hosts {
		if hosts[index].Spec.ConsumerRef != nil && hosts[index].Spec.ConsumerRef.UID == machine.UID {
			return hosts[index].DeepCopy(), nil
		}
	}
	if machine.Spec.HostRef != nil {
		selected := &infrav1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKey{Name: machine.Spec.HostRef.Name}, selected); err != nil {
			return nil, err
		}
		if selected.Spec.ConsumerRef != nil && selected.Spec.ConsumerRef.UID == machine.UID {
			return selected, nil
		}
		if host.Classify(selected.Spec) != host.Available || !host.Matches(selected.Labels, selected.Spec, machine.Spec.HostSelector) {
			return nil, host.ErrNoEligibleHost
		}
		return selected, nil
	}
	selected, err := host.SelectFresh(hosts, machine.Spec.HostSelector)
	return selected, err
}

func (r *TartMachineReconciler) reconcileDeletion(ctx context.Context, machine *infrav1alpha1.TartMachine) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(machine, tartMachineFinalizer) {
		return ctrl.Result{}, nil
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
	return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "Talos shutdown and Host stop confirmation are not implemented; the Host claim is retained safely.", shutdownConfirmationRequeue)
}

var errMachineHasAmbiguousHostClaims = errors.New("machine has ambiguous host claims")

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
	}

	var claimed *infrav1alpha1.TartHost
	for index := range allHosts.Items {
		candidate := &allHosts.Items[index]
		if candidate.Spec.ConsumerRef == nil || candidate.Spec.ConsumerRef.UID != machine.UID {
			continue
		}
		if claimed != nil && claimed.Name != candidate.Name {
			return nil, errMachineHasAmbiguousHostClaims
		}
		claimed = candidate
	}
	if claimed != nil {
		return claimed, nil
	}
	if statusHost != nil && statusHost.Spec.ConsumerRef != nil && statusHost.Spec.ConsumerRef.UID == machine.UID {
		return statusHost, nil
	}
	return nil, nil
}

func (r *TartMachineReconciler) report(ctx context.Context, machine *infrav1alpha1.TartMachine, reason, message string) (ctrl.Result, error) {
	original := machine.DeepCopy()
	setCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition, metav1.ConditionFalse, reason, message, machine.Generation)
	machine.Status.ObservedGeneration = machine.Generation
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartMachineReconciler) reportAndRequeue(ctx context.Context, machine *infrav1alpha1.TartMachine, reason, message string, after time.Duration) (ctrl.Result, error) {
	result, err := r.report(ctx, machine, reason, message)
	result.RequeueAfter = after
	return result, err
}

func (r *TartMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartMachine{}).
		Named("tartmachine").
		Complete(r)
}
