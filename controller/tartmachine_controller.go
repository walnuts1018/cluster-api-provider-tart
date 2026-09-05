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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/boot"
	"github.com/walnuts1018/cluster-api-provider-tart/bootstrap"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/host"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	tartMachineFinalizer        = "tart.cluster.x-k8s.io/machine-lifecycle"
	shutdownConfirmationRequeue = 30 * time.Second
	shutdownConfirmationDelay   = 30 * time.Second
	talosReconcileTimeout       = 20 * time.Second
	talosRequeue                = 30 * time.Second
)

var (
	errBootstrapDataUnavailable = errors.New("bootstrap data is unavailable")
	errCAPIProviderIDMismatch   = errors.New("CAPI Machine provider ID does not match TartHost")
	errHostSelectionMismatch    = errors.New("allocated TartHost does not match CAPI Machine placement")
)

// TartMachineReconcilerはHost claim、Talosの初回configuration apply、認証済みAPIの起動確認を担当する。初回provisioning後のmutableな変更はUpdate Extensionへ委譲する。
type TartMachineReconciler struct {
	client.Client
	// ManagementNamespaceはRedfish credential SecretとTalos recovery Secretを解決するprovider管理namespaceである。TartHostのSpecからnamespaceを受け取らない。
	ManagementNamespace string
	// TalosDialerはReprovision flowのTalos接続を差し替えるための境界である。未設定の場合は実際のTalos gRPC clientを使う。
	TalosDialer TalosDialer
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

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

	return r.reconcileProvisioning(ctx, &machine)
}

func (r *TartMachineReconciler) reconcileProvisioning(ctx context.Context, machine *infrav1alpha1.TartMachine) (ctrl.Result, error) {
	allHosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, allHosts); err != nil {
		return ctrl.Result{}, err
	}
	if host.HasIdentityConflictForAny(allHosts.Items) {
		return r.report(ctx, machine, infrav1alpha1.ReasonIdentityConflict, "Stable Host identity is duplicated; allocation is stopped until the conflict is resolved.")
	}

	selected, err := r.observedOrSelectedHost(ctx, machine, allHosts.Items)
	if err != nil {
		if errors.Is(err, errCAPIMachineUnavailable) {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The corresponding CAPI Machine is not available to determine Host placement.", 30*time.Second)
		}
		if errors.Is(err, errHostSelectionMismatch) {
			return r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The allocated TartHost does not match the CAPI Machine Failure Domain or HostSelector.")
		}
		if errors.Is(err, host.ErrNoEligibleHost) {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonNoEligibleHost, "No eligible fresh TartHost is available for this Machine.", 30*time.Second)
		}
		if apierrors.IsNotFound(err) {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonHostNotFound, "The referenced TartHost was not found.", 30*time.Second)
		}
		return ctrl.Result{}, err
	}
	if selected == nil {
		return r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The observed Host binding is unavailable.")
	}

	if selected.Spec.HostID == "" {
		return r.report(ctx, machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost has no persistent identity yet.")
	}
	hostID, err := parseHostID(selected.Spec.HostID)
	if err != nil {
		return r.report(ctx, machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost identity is invalid.")
	}
	providerID, err := host.ProviderID(hostID)
	if err != nil {
		return r.report(ctx, machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost identity is invalid.")
	}
	if !machine.Spec.ProviderID.IsZero() && machine.Spec.ProviderID != providerID {
		return r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The existing ProviderID does not match the allocated TartHost identity.")
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
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonHostClaimConflict, "The selected TartHost was claimed concurrently; allocation will be retried against current state.", 2*time.Second)
		}
		return ctrl.Result{}, err
	}

	if machine.Spec.ProviderID.IsZero() {
		original := machine.DeepCopy()
		machine.Spec.ProviderID = providerID
		if err := r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.syncCAPIProviderID(ctx, machine, providerID); err != nil {
		if errors.Is(err, errCAPIProviderIDMismatch) {
			return r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The CAPI Machine ProviderID does not match the allocated TartHost identity.")
		}
		return ctrl.Result{}, err
	}

	statusOriginal := machine.DeepCopy()
	machine.Status.HostRef = &corev1.LocalObjectReference{Name: selected.Name}
	if endpoint := hostTalosEndpoint(selected); endpoint != "" {
		machine.Status.Addresses = hostAddresses(endpoint)
	}
	if selected.Spec.FailureDomain != "" {
		machine.Status.FailureDomain = selected.Spec.FailureDomain
	}
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(statusOriginal)); err != nil {
		return ctrl.Result{}, err
	}
	configuration, err := r.bootstrapConfiguration(ctx, machine)
	if err != nil {
		if errors.Is(err, errBootstrapDataUnavailable) {
			return r.reportTalosStatus(ctx, machine,
				metav1.ConditionFalse, "BootstrapDataUnavailable", "Talos provisioning is waiting for bootstrap data.",
				metav1.ConditionFalse, "BootstrapDataUnavailable", "Talos provisioning is waiting for bootstrap data.",
				"BootstrapDataUnavailable", "Talos version cannot be verified before provisioning.",
				"BootstrapDataUnavailable", "The immutable Bootstrap Secret is not available yet.",
				talosRequeue)
		}
		return ctrl.Result{}, err
	}

	return r.reconcileTalos(ctx, machine, selected, configuration)
}

func (r *TartMachineReconciler) syncCAPIProviderID(ctx context.Context, machine *infrav1alpha1.TartMachine, providerID hostdomain.ProviderID) error {
	clusterMachine, err := findCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if errors.Is(err, errCAPIMachineIdentityMismatch) {
		return errCAPIProviderIDMismatch
	}
	if errors.Is(err, errCAPIMachineUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	if clusterMachine.Spec.ProviderID != "" {
		if clusterMachine.Spec.ProviderID != providerID.String() {
			return errCAPIProviderIDMismatch
		}
		return nil
	}
	original := clusterMachine.DeepCopy()
	clusterMachine.Spec.ProviderID = providerID.String()
	return r.Patch(ctx, clusterMachine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func (r *TartMachineReconciler) bootstrapConfiguration(ctx context.Context, machine *infrav1alpha1.TartMachine) ([]byte, error) {
	clusterMachine, err := findCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if errors.Is(err, errCAPIMachineUnavailable) {
		return nil, errBootstrapDataUnavailable
	}
	if err != nil {
		return nil, err
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
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "EndpointUnavailable", "The Host has no reachable Talos maintenance endpoint.",
			metav1.ConditionFalse, "EndpointUnavailable", "Talos installation has not started because the Host endpoint is not observed.",
			"EndpointUnavailable", "Talos version cannot be verified before the Host is reachable.",
			"EndpointUnavailable", "The Host Talos endpoint is not available yet.",
			talosRequeue)
	}
	// 承認済みReprovisionでHostが旧Talos installationを保持している間は、まずそのinstallationを検証してresetする。
	// 旧installationは新しいconfigurationのCAでは認証できないため、この分岐を経ずにfresh provisioningへ進むことはない。
	if result, handled, err := r.reconcileReprovision(ctx, machine, selected, endpoint); handled {
		return result, err
	}
	// installationのrecovery identityは、configurationをHostへ渡すより前に確立する。
	// Machine削除の瞬間にSecretを退避する設計にせず、Hostがそのinstallationを保持する間ずっと参照できるようにする。
	if err := r.ensureTalosIdentityBinding(ctx, machine, selected); err != nil {
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonRecoveryIdentityUnavailable, "The Talos recovery identity for this installation could not be established.",
			metav1.ConditionFalse, infrav1alpha1.ReasonRecoveryIdentityUnavailable, "Talos installation is stopped until its recovery identity is persisted.",
			infrav1alpha1.ReasonRecoveryIdentityUnavailable, "Talos version cannot be verified before the recovery identity is persisted.",
			infrav1alpha1.ReasonRecoveryIdentityUnavailable, "The Talos recovery Secret could not be established for this Host.",
			talosRequeue)
	}
	if result, handled, err := r.reconcileAuthenticatedTalos(ctx, machine, endpoint, configuration); handled {
		return result, err
	}
	return r.reconcileMaintenanceTalos(ctx, machine, selected, endpoint, configuration)
}

func (r *TartMachineReconciler) reconcileAuthenticatedTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, endpoint string, configuration []byte) (ctrl.Result, bool, error) {
	connectionContext, cancel := context.WithTimeout(ctx, talosReconcileTimeout)
	authenticated, authErr := talos.DialAuthenticatedFromConfiguration(connectionContext, endpoint, configuration)
	cancel()
	if authErr != nil {
		return ctrl.Result{}, false, nil //nolint:nilerr // failed authenticated access falls through to maintenance mode observation.
	}

	versionContext, versionCancel := context.WithTimeout(ctx, talosReconcileTimeout)
	version, versionErr := authenticated.Version(versionContext)
	versionCancel()
	if versionErr != nil {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		result, err := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "TalosUnreachable", "The authenticated Talos API could not be queried.",
			metav1.ConditionFalse, "TalosUnreachable", "Talos provisioning has not been confirmed.",
			"TalosUnreachable", "The desired Talos version cannot be verified.",
			"TalosUnreachable", "The authenticated Talos API is not reachable.",
			talosRequeue)
		return result, true, err
	}

	schematicContext, schematicCancel := context.WithTimeout(ctx, talosReconcileTimeout)
	observedSchematicID, schematicErr := authenticated.SchematicID(schematicContext)
	schematicCancel()
	if schematicErr != nil {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, "",
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, "SchematicUnavailable", "The Talos schematic identity could not be observed.",
			metav1.ConditionFalse, "SchematicUnavailable", "The desired Talos image cannot be verified without its schematic identity.",
			talosRequeue)
		return result, true, err
	}

	if machine.Spec.Image.Version != "" && version.Tag == machine.Spec.Image.Version && observedSchematicID == machine.Spec.Image.SchematicID {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionTrue, "UpToDate", "The observed Talos version and schematic match the desired image.",
			metav1.ConditionTrue, "Ready", "The Host is running the desired Talos version and schematic.",
			0)
		return result, true, err
	}

	mismatchReason := "VersionMismatch"
	mismatchMessage := "The observed Talos version does not match the desired version."
	readyMessage := "The Host is running Talos, but not the desired version or schematic."
	if version.Tag == machine.Spec.Image.Version && observedSchematicID != machine.Spec.Image.SchematicID {
		mismatchReason = "SchematicMismatch"
		mismatchMessage = "The observed Talos schematic does not match the desired schematic."
		readyMessage = "The Host is running the desired Talos version, but not the desired schematic."
	}
	previousUpToDate := meta.FindStatusCondition(machine.Status.Conditions, infrav1alpha1.TartMachineTalosUpToDateCondition)
	wasUpToDate := previousUpToDate != nil && previousUpToDate.Status == metav1.ConditionTrue
	if wasUpToDate && machine.Status.TalosVersion == machine.Spec.Image.Version && machine.Status.TalosSchematicID == machine.Spec.Image.SchematicID {
		mismatchReason = infrav1alpha1.ReasonRolledBack
		mismatchMessage = "The previously observed Talos image is no longer running; automatic rollback recovery is stopped."
		readyMessage = "The Host no longer reports the previously observed desired Talos image."
	}
	if closeErr := authenticated.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
	}
	result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
		metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
		metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
		metav1.ConditionFalse, mismatchReason, mismatchMessage,
		metav1.ConditionFalse, mismatchReason, readyMessage,
		0)
	return result, true, err
}

func (r *TartMachineReconciler) reconcileMaintenanceTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, endpoint string, configuration []byte) (ctrl.Result, error) {
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
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "MaintenanceUnavailable", "The Talos maintenance API is unavailable.",
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
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, "ConfigurationInvalid", "The Talos machine configuration could not be prepared for the desired installer image.", "The desired Talos installer image could not be applied to the machine configuration.")
	}
	effectiveConfiguration, err = talos.SetProviderID(effectiveConfiguration, machine.Spec.ProviderID.String())
	if err != nil {
		reason := "ConfigurationInvalid"
		message := "The Talos machine configuration could not be prepared with the allocated ProviderID."
		if errors.Is(err, talos.ErrProviderIDConflict) {
			reason = "ConfigurationConflict"
			message = "The Talos machine configuration contains a ProviderID that conflicts with the allocated Host."
		}
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, reason, message, message)
	}
	if err := bootstrap.ValidateMachineConfiguration(effectiveConfiguration); err != nil {
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, "ConfigurationInvalid", "The complete Talos machine configuration failed client-side validation.", "The complete Talos machine configuration is invalid.")
	}
	if err := maintenance.ApplyConfiguration(ctx, effectiveConfiguration); err != nil {
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, "ConfigurationApplyFailed", "The complete Talos machine configuration could not be applied.", "The Talos maintenance API rejected the machine configuration.")
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

func (r *TartMachineReconciler) reportMaintenanceConfigurationError(ctx context.Context, machine *infrav1alpha1.TartMachine, maintenance *talos.Client, reason, message, readyMessage string) (ctrl.Result, error) {
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	return r.reportTalosStatus(ctx, machine,
		metav1.ConditionFalse, reason, message,
		metav1.ConditionFalse, reason, "Talos installation has not been confirmed.",
		reason, "Talos version cannot be verified before installation.",
		reason, readyMessage,
		talosRequeue)
}

func hostTalosEndpoint(host *infrav1alpha1.TartHost) string {
	if endpoint := host.Spec.TalosAPIAddress.String(); endpoint != "" {
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

func isProvisioned(machine *infrav1alpha1.TartMachine) bool {
	return machine.Status.Initialization.Provisioned != nil && *machine.Status.Initialization.Provisioned
}

func (r *TartMachineReconciler) reportTalosStatus(ctx context.Context, machine *infrav1alpha1.TartMachine,
	reachableStatus metav1.ConditionStatus, reachableReason, reachableMessage string,
	provisionedStatus metav1.ConditionStatus, provisionedReason, provisionedMessage string,
	upToDateReason, upToDateMessage, readyReason, readyMessage string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	return r.reportTalosStatusWithVersion(ctx, machine, "", "", reachableStatus, reachableReason, reachableMessage, provisionedStatus, provisionedReason, provisionedMessage, metav1.ConditionFalse, upToDateReason, upToDateMessage, metav1.ConditionFalse, readyReason, readyMessage, requeueAfter)
}

func (r *TartMachineReconciler) reportTalosStatusWithVersion(ctx context.Context, machine *infrav1alpha1.TartMachine, talosVersion string,
	talosSchematicID string,
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
	if talosSchematicID != "" {
		machine.Status.TalosSchematicID = talosSchematicID
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
	capiMachine, err := findCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return nil, err
	}
	failureDomain := capiMachine.Spec.FailureDomain
	if machine.Status.HostRef != nil {
		observed := &infrav1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKey{Name: machine.Status.HostRef.Name}, observed); err != nil {
			return nil, err
		}
		if observed.Spec.ConsumerRef == nil || observed.Spec.ConsumerRef.UID != machine.UID {
			return nil, nil
		}
		if !host.MatchesForFailureDomain(observed.Labels, observed.Spec, machine.Spec.HostSelector, failureDomain) {
			return nil, errHostSelectionMismatch
		}
		return observed, nil
	}
	for index := range hosts {
		if hosts[index].Spec.ConsumerRef != nil && hosts[index].Spec.ConsumerRef.UID == machine.UID {
			if !host.MatchesForFailureDomain(hosts[index].Labels, hosts[index].Spec, machine.Spec.HostSelector, failureDomain) {
				return nil, errHostSelectionMismatch
			}
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
		// 明示的なspec.hostRefは、reuse approvalとreuse modeが揃ったReusable Hostを再利用する唯一の経路である。自動選択経路(SelectFreshForFailureDomain)はAvailable Hostしか選ばない。
		eligibility := host.Classify(selected.Spec)
		if (eligibility != host.Available && eligibility != host.Reusable) || !host.MatchesForFailureDomain(selected.Labels, selected.Spec, machine.Spec.HostSelector, failureDomain) {
			return nil, host.ErrNoEligibleHost
		}
		return selected, nil
	}
	selected, err := host.SelectFreshForFailureDomain(hosts, machine.Spec.HostSelector, failureDomain)
	return selected, err
}

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

	configuration, configurationErr := r.bootstrapConfiguration(ctx, machine)
	if configurationErr != nil && !errors.Is(configurationErr, errBootstrapDataUnavailable) {
		return ctrl.Result{}, configurationErr
	}
	if !hasShutdownRequest(machine) {
		requested, requestErr := requestHostShutdown(ctx, selected, configuration)
		if requestErr != nil {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host could not be shut down safely; the Machine finalizer remains.", shutdownConfirmationRequeue)
		}
		if !requested {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host is not reachable through a verified Talos API; shutdown has not been confirmed.", shutdownConfirmationRequeue)
		}
		return r.reportAndRequeue(ctx, machine, "ShutdownRequested", "Talos shutdown was requested; the Host claim remains until API unreachability is observed.", shutdownConfirmationRequeue)
	}

	if !shutdownRequestSettled(machine) {
		return r.reportAndRequeue(ctx, machine, "ShutdownRequested", "The Host API is unreachable after shutdown request; waiting for the confirmation interval before retention.", shutdownConfirmationRequeue)
	}

	stopped, observationErr := r.observeHostStopped(ctx, selected, configuration)
	if observationErr != nil {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host stop state could not be verified; the Machine finalizer remains.", shutdownConfirmationRequeue)
	}
	if !stopped {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host still responds to a Talos API; the Host claim remains.", shutdownConfirmationRequeue)
	}
	consumer := corev1.ObjectReference{
		APIVersion: infrav1alpha1.GroupVersion.String(),
		Kind:       tartMachineKind,
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
	previous := r.previousConsumerRef(ctx, machine, consumer)
	if err := host.Retain(ctx, r.Client, selected, consumer, previous); err != nil {
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The Host retention record could not be written atomically; the Machine finalizer remains.", shutdownConfirmationRequeue)
	}

	original := machine.DeepCopy()
	controllerutil.RemoveFinalizer(machine, tartMachineFinalizer)
	if err := r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// capiDeletionDrainCompleteはprovider resourceの削除前にCAPI Machine controllerがdrainとvolume detachを完了したことを確認する。pre-terminate hookがあるcontrol planeでは、そのhook解除後にCAPIがinfra削除段階へ進んだことも同時に確認できる。
func (r *TartMachineReconciler) capiDeletionDrainComplete(ctx context.Context, machine *infrav1alpha1.TartMachine) (bool, error) {
	capiMachine, err := findCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return false, err
	}
	return capiMachineDeletionDrainComplete(capiMachine), nil
}

func capiMachineDeletionDrainComplete(capiMachine *clusterv1.Machine) bool {
	if capiMachine == nil || capiMachine.DeletionTimestamp.IsZero() {
		return false
	}
	condition := meta.FindStatusCondition(capiMachine.Status.Conditions, clusterv1.MachineDeletingCondition)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		return false
	}
	switch condition.Reason {
	case clusterv1.MachineDeletingWaitingForInfrastructureDeletionReason,
		clusterv1.MachineDeletingWaitingForBootstrapDeletionReason,
		clusterv1.MachineDeletingDeletingNodeReason,
		clusterv1.MachineDeletingDeletionCompletedReason:
		return true
	default:
		return false
	}
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
		if statusHost.Spec.ConsumerRef == nil || statusHost.Spec.ConsumerRef.UID != machine.UID {
			return nil, errMachineHostBindingLost
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

var errMachineHostBindingLost = errors.New("machine host binding was lost before deletion completed")

func hasShutdownRequest(machine *infrav1alpha1.TartMachine) bool {
	condition := meta.FindStatusCondition(machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	return condition != nil && condition.Reason == "ShutdownRequested"
}

func shutdownRequestSettled(machine *infrav1alpha1.TartMachine) bool {
	condition := meta.FindStatusCondition(machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	return condition != nil && condition.Reason == "ShutdownRequested" && !condition.LastTransitionTime.IsZero() && time.Since(condition.LastTransitionTime.Time) >= shutdownConfirmationDelay
}

func requestHostShutdown(ctx context.Context, selected *infrav1alpha1.TartHost, configuration []byte) (bool, error) {
	endpoint := hostTalosEndpoint(selected)
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

	maintenance, err := talos.DialMaintenance(ctx, endpoint)
	if err != nil {
		return false, nil //nolint:nilerr // maintenance mode may not be reachable until the Host finishes shutting down.
	}
	identity, identityErr := maintenance.Inventory(ctx)
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
		return false, errHostIdentityMismatch
	}
	shutdownErr := maintenance.Shutdown(ctx)
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	if shutdownErr != nil {
		return false, shutdownErr
	}
	return true, nil
}

func (r *TartMachineReconciler) observeHostStopped(ctx context.Context, selected *infrav1alpha1.TartHost, configuration []byte) (bool, error) {
	if selected.Spec.Power.Backend == infrav1alpha1.PowerBackendRedfish {
		state, err := r.redfishPowerState(ctx, selected)
		if err != nil {
			return false, err
		}
		return state == boot.PowerStateOff, nil
	}
	endpoint := hostTalosEndpoint(selected)
	if endpoint == "" {
		return false, errHostEndpointUnavailable
	}
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
			// TLS接続が成立した状態でのAPI観測失敗は、shutdown中または一時的なTalos障害と区別できない。
			// maintenance endpointへ接続できないことだけを根拠に停止済みへ遷移させない。
			return false, fmt.Errorf("authenticated Talos API stopped responding before shutdown was confirmed: %w", versionErr)
		}
	}

	maintenance, err := talos.DialMaintenance(ctx, endpoint)
	if err != nil {
		return true, nil //nolint:nilerr // 確認待ち時間の経過後はTalos endpointの消失をWoL/manualの停止証拠として扱う。
	}
	identity, identityErr := maintenance.Inventory(ctx)
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	if identityErr != nil {
		return false, nil //nolint:nilerr // identity observation must succeed before declaring the Host stopped.
	}
	if !identity.HasMAC(selected.Spec.MACAddress) {
		return false, errHostIdentityMismatch
	}
	return false, nil
}

func (r *TartMachineReconciler) previousConsumerRef(ctx context.Context, machine *infrav1alpha1.TartMachine, consumer corev1.ObjectReference) infrav1alpha1.PreviousConsumerRef {
	previous := infrav1alpha1.PreviousConsumerRef{
		Namespace: consumer.Namespace,
		Name:      consumer.Name,
		UID:       consumer.UID,
	}
	capiMachine, err := findCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return previous
	}
	var cluster clusterv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: capiMachine.Namespace, Name: capiMachine.Spec.ClusterName}, &cluster); err != nil {
		return previous
	}
	previous.ClusterID = ""
	if ref := cluster.Spec.InfrastructureRef; ref.APIGroup == infrav1alpha1.GroupVersion.Group && ref.Kind == tartClusterKind && ref.Name != "" {
		var tartCluster infrav1alpha1.TartCluster
		if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, &tartCluster); err == nil {
			previous.ClusterID = tartCluster.Spec.ClusterID
		}
	}
	return previous
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
		Watches(&infrav1alpha1.TartHost{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			hostObject, ok := obj.(*infrav1alpha1.TartHost)
			if !ok || hostObject.Spec.ConsumerRef == nil || hostObject.Spec.ConsumerRef.Namespace == "" || hostObject.Spec.ConsumerRef.Name == "" || hostObject.Spec.ConsumerRef.UID == "" {
				return nil
			}
			return []reconcile.Request{{Namespace: hostObject.Spec.ConsumerRef.Namespace, Name: hostObject.Spec.ConsumerRef.Name}}
		})).
		Named("tartmachine").
		Complete(r)
}
