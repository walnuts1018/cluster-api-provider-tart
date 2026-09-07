package tartmachine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	kubernetesadapter "github.com/walnuts1018/cluster-api-provider-tart/adapter/kubernetes"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/runtimeextension"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	machinedomain "github.com/walnuts1018/cluster-api-provider-tart/domain/machine"
	"github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
	machineusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/machine"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	tartMachineFinalizer        = "tart.cluster.x-k8s.io/machine-lifecycle"
	shutdownConfirmationRequeue = 30 * time.Second
	shutdownConfirmationDelay   = 30 * time.Second
	talosReconcileTimeout       = 20 * time.Second
	talosRequeue                = 30 * time.Second
	maintenanceDialTimeout      = 10 * time.Second
	maintenanceObserveTimeout   = 10 * time.Second
	maintenanceActionTimeout    = 20 * time.Second
)

var (
	ErrBootstrapDataUnavailable = errors.New("bootstrap data is unavailable")
	// ErrShutdownStateUnverifiableは、WoL/Manualなど独立したpower-state observerを持たないHostで停止検証ができないことを示す。
	ErrShutdownStateUnverifiable = errors.New("host shutdown state is unverifiable without an independent power-state observer")
	errCAPIProviderIDMismatch    = errors.New("CAPI Machine provider ID does not match TartHost")
	errHostSelectionMismatch     = errors.New("allocated TartHost does not match CAPI Machine placement")
)

// TartMachineReconcilerはHost claim、Talosの初回configuration apply、認証済みAPIの起動確認を担当する。初回provisioning後のmutableな変更はUpdate Extensionへ委譲する。
type TartMachineReconciler struct {
	client.Client
	// ManagementNamespaceはRedfish credential SecretとTalos recovery Secretを解決するprovider管理namespaceである。TartHostのSpecからnamespaceを受け取らない。
	ManagementNamespace string
	// TalosDialerはReprovision flowのTalos接続を差し替えるための境界である。未設定の場合は実際のTalos gRPC clientを使う。
	TalosDialer TalosDialer
}

// NewTartMachineReconcilerはclientのみを設定したTartMachineReconcilerを構築する。ManagementNamespaceや
// TalosDialerなどのoptional fieldはDI wiringの呼び出し元が必要に応じて後から設定する。
func NewTartMachineReconciler(c client.Client) *TartMachineReconciler {
	return &TartMachineReconciler{Client: c}
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
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
	if controller.IsPaused(&machine) {
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
	selected, result, handled, err := r.reconcileProvisioningHost(ctx, machine)
	if handled {
		return result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	configuration, result, handled, err := r.reconcileProvisioningStatus(ctx, machine, selected)
	if handled {
		return result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileTalos(ctx, machine, selected, configuration)
}

func (r *TartMachineReconciler) reconcileProvisioningHost(ctx context.Context, machine *infrav1alpha1.TartMachine) (*infrav1alpha1.TartHost, ctrl.Result, bool, error) {
	allHosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, allHosts); err != nil {
		return nil, ctrl.Result{}, false, err
	}
	if hostusecase.HasIdentityConflictForAny(allHosts.Items) {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonIdentityConflict, "Stable Host identity is duplicated; allocation is stopped until the conflict is resolved.")
	}

	selected, err := r.observedOrSelectedHost(ctx, machine, allHosts.Items)
	if err != nil {
		return r.reconcileProvisioningHostSelectionError(ctx, machine, err)
	}
	if selected == nil {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The observed Host binding is unavailable.")
	}

	if selected.Spec.HostID == "" {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost has no persistent identity yet.")
	}
	hostID, err := hostdomain.ParseHostID(selected.Spec.HostID)
	if err != nil {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost identity is invalid.")
	}
	providerID, err := hostdomain.NewProviderID(hostID)
	if err != nil {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost identity is invalid.")
	}
	if !machine.Spec.ProviderID.IsZero() && machine.Spec.ProviderID != providerID {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The existing ProviderID does not match the allocated TartHost identity.")
	}

	consumer := corev1.ObjectReference{
		APIVersion: infrav1alpha1.GroupVersion.String(),
		Kind:       controller.TartMachineKind,
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
	// Host claimはselection predicateを含めて同じresourceVersion上で検証する。Retained Hostの自動claimやFailureDomain変更の競合をfail-closedで拒否する。
	capiMachineForClaim, capiClaimErr := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	failureDomainForClaim := ""
	if capiClaimErr == nil && capiMachineForClaim != nil {
		failureDomainForClaim = capiMachineForClaim.Spec.FailureDomain
	}
	claimReq := hostusecase.ClaimRequest{
		Consumer:       consumer,
		ExpectedHostID: selected.Spec.HostID,
		Selector:       machine.Spec.HostSelector,
		FailureDomain:  failureDomainForClaim,
		Mode:           hostusecase.ClaimFreshAutomatic,
	}
	if err := kubernetesadapter.NewTartHostRepository(r.Client).ClaimHostWithRequest(ctx, selected, claimReq); err != nil {
		if errors.Is(err, hostusecase.ErrClaimConflict) || errors.Is(err, hostusecase.ErrHostNoLongerEligible) || errors.Is(err, hostusecase.ErrReuseApprovalRequired) || errors.Is(err, hostusecase.ErrHostIdentityChanged) || errors.Is(err, hostusecase.ErrHostSelectionMismatch) {
			reason := infrav1alpha1.ReasonHostClaimConflict
			msg := "The selected TartHost was claimed concurrently or is no longer eligible; allocation will be retried against current state."
			if errors.Is(err, hostusecase.ErrReuseApprovalRequired) {
				reason = infrav1alpha1.ReasonReuseApprovalRequired
				msg = "The selected TartHost is retained and requires explicit reuse approval; automatic allocation is blocked."
			} else if errors.Is(err, hostusecase.ErrHostSelectionMismatch) || errors.Is(err, errHostSelectionMismatch) {
				reason = infrav1alpha1.ReasonHostMismatch
				msg = "The selected TartHost no longer matches the Machine placement constraints."
			}
			result, reportErr := r.reportAndRequeue(ctx, machine, reason, msg, 2*time.Second)
			return nil, result, true, reportErr
		}
		return nil, ctrl.Result{}, false, err
	}
	// ProviderIDはclaim成功後のfresh Hostからのみ導出する。stale snapshot由来の値を再利用しない。
	if strings.TrimSpace(selected.Spec.HostID) == "" {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, "HostIdentityInvalid", "The claimed Host has an invalid HostID; ProviderID publication is stopped.")
	}
	freshHostID, err := hostdomain.ParseHostID(selected.Spec.HostID)
	if err != nil {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, "HostIdentityInvalid", "The claimed Host has an invalid HostID; ProviderID publication is stopped.")
	}
	freshProviderID, err := hostdomain.NewProviderID(freshHostID)
	if err != nil {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, "ProviderIDInvalid", "A ProviderID could not be derived from the claimed Host identity.")
	}
	providerID = freshProviderID

	if machine.Spec.ProviderID.IsZero() {
		original := machine.DeepCopy()
		machine.Spec.ProviderID = providerID
		if err := r.Patch(ctx, machine, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return nil, ctrl.Result{}, false, err
		}
	}
	if err := r.syncCAPIProviderID(ctx, machine, providerID); err != nil {
		if errors.Is(err, errCAPIProviderIDMismatch) {
			return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The CAPI Machine ProviderID does not match the allocated TartHost identity.")
		}
		return nil, ctrl.Result{}, false, err
	}

	return selected, ctrl.Result{}, false, nil
}

func (r *TartMachineReconciler) reconcileProvisioningHostSelectionError(ctx context.Context, machine *infrav1alpha1.TartMachine, err error) (*infrav1alpha1.TartHost, ctrl.Result, bool, error) {
	if errors.Is(err, controller.ErrCAPIMachineUnavailable) {
		result, reportErr := r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The corresponding CAPI Machine is not available to determine Host placement.", 30*time.Second)
		return nil, result, true, reportErr
	}
	if errors.Is(err, controller.ErrCAPIMachineReferenceMismatch) || errors.Is(err, controller.ErrCAPIMachineAmbiguous) {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, "CAPIMachineInvalid", "The CAPI Machine reference is structurally invalid; allocation is stopped.")
	}
	if errors.Is(err, errHostSelectionMismatch) {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The allocated TartHost does not match the CAPI Machine Failure Domain or HostSelector.")
	}
	if errors.Is(err, hostusecase.ErrNoEligibleHost) {
		result, reportErr := r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonNoEligibleHost, "No eligible fresh TartHost is available for this Machine.", 30*time.Second)
		return nil, result, true, reportErr
	}
	if apierrors.IsNotFound(err) {
		result, reportErr := r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonHostNotFound, "The referenced TartHost was not found.", 30*time.Second)
		return nil, result, true, reportErr
	}
	return nil, ctrl.Result{}, false, err
}

func (r *TartMachineReconciler) reconcileProvisioningStatus(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost) ([]byte, ctrl.Result, bool, error) {
	statusOriginal := machine.DeepCopy()
	machine.Status.HostRef = &corev1.LocalObjectReference{Name: selected.Name}
	if endpoint := controller.HostTalosEndpoint(selected); endpoint != "" {
		machine.Status.Addresses = controller.HostAddresses(endpoint)
	}
	if selected.Spec.FailureDomain != "" {
		machine.Status.FailureDomain = selected.Spec.FailureDomain
	}
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(statusOriginal)); err != nil {
		return nil, ctrl.Result{}, false, err
	}
	configuration, err := r.BootstrapConfiguration(ctx, machine)
	if err != nil {
		if errors.Is(err, ErrBootstrapDataUnavailable) {
			result, reportErr := r.reportTalosStatus(ctx, machine,
				metav1.ConditionFalse, "BootstrapDataUnavailable", "Talos provisioning is waiting for bootstrap data.",
				metav1.ConditionFalse, "BootstrapDataUnavailable", "Talos provisioning is waiting for bootstrap data.",
				"BootstrapDataUnavailable", "Talos version cannot be verified before provisioning.",
				"BootstrapDataUnavailable", "The immutable Bootstrap Secret is not available yet.",
				talosRequeue)
			return nil, result, true, reportErr
		}
		return nil, ctrl.Result{}, false, err
	}

	return configuration, ctrl.Result{}, false, nil
}

func (r *TartMachineReconciler) syncCAPIProviderID(ctx context.Context, machine *infrav1alpha1.TartMachine, providerID hostdomain.ProviderID) error {
	clusterMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if errors.Is(err, controller.ErrCAPIMachineIdentityMismatch) {
		return errCAPIProviderIDMismatch
	}
	if errors.Is(err, controller.ErrCAPIMachineUnavailable) {
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

func (r *TartMachineReconciler) BootstrapConfiguration(ctx context.Context, machine *infrav1alpha1.TartMachine) ([]byte, error) {
	clusterMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if errors.Is(err, controller.ErrCAPIMachineUnavailable) {
		return nil, ErrBootstrapDataUnavailable
	}
	if err != nil {
		return nil, err
	}
	ref := clusterMachine.Spec.Bootstrap.ConfigRef
	if ref.Name == "" || ref.Kind != controller.TartBootstrapConfigKind || ref.APIGroup != bootstrapv1alpha1.GroupVersion.Group {
		return nil, ErrBootstrapDataUnavailable
	}

	config := &bootstrapv1alpha1.TartBootstrapConfig{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, config); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrBootstrapDataUnavailable
		}
		return nil, err
	}
	if strings.TrimSpace(config.Status.DataSecretName) == "" {
		return nil, ErrBootstrapDataUnavailable
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: config.Status.DataSecretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrBootstrapDataUnavailable
		}
		return nil, err
	}
	clusterName := config.Labels[bootstrap.ClusterNameLabel]
	if !bootstrap.IsContractSecret(secret, clusterName, config.UID) {
		return nil, ErrBootstrapDataUnavailable
	}
	configuration, ok := secret.Data[bootstrap.BootstrapSecretKey]
	if !ok || len(bytes.TrimSpace(configuration)) == 0 {
		return nil, ErrBootstrapDataUnavailable
	}
	return bytes.Clone(configuration), nil
}

func (r *TartMachineReconciler) reconcileTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, configuration []byte) (ctrl.Result, error) {
	endpoint := controller.HostTalosEndpoint(selected)
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
		if errors.Is(authErr, talos.ErrTalosConfigurationInvalid) || errors.Is(authErr, talos.ErrEndpointEmpty) {
			result, err := r.reportTalosStatus(ctx, machine,
				metav1.ConditionFalse, "TalosCredentialInvalid", "The Talos machine configuration cannot be used to establish an authenticated connection.",
				metav1.ConditionFalse, "TalosCredentialInvalid", "Talos installation is stopped until the machine configuration is valid.",
				"TalosCredentialInvalid", "The desired Talos credentials cannot be verified.",
				"TalosCredentialInvalid", "The Talos machine configuration is invalid for authenticated access.",
				talosRequeue)
			return result, true, err
		}
		ctrl.LoggerFrom(ctx).Info("authenticated Talos dial failed; falling back to maintenance mode observation", "error", authErr.Error(), "endpoint", endpoint)
		return ctrl.Result{}, false, nil
	}

	versionContext, versionCancel := context.WithTimeout(ctx, talosReconcileTimeout)
	version, versionErr := authenticated.Version(versionContext)
	versionCancel()
	if versionErr != nil {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		// grpc.Dialはlazy connectionのため、CAの不一致(nodeがまだmaintenance modeで
		// この設定を適用されていない場合など)はDialAuthenticatedFromConfiguration自体ではなく、
		// 最初のRPCであるVersion()の呼び出し時にcodes.Unavailableとして顕在化する。この場合は
		// authErrと同様にmaintenance mode観測へfallbackしなければ、node未設定のまま
		// 恒久的にTalosUnreachableへ張り付いてしまう。
		if status.Code(versionErr) == codes.Unavailable {
			ctrl.LoggerFrom(ctx).Info("authenticated Talos Version() call unavailable; falling back to maintenance mode observation", "error", versionErr.Error(), "endpoint", endpoint)
			return ctrl.Result{}, false, nil
		}
		ctrl.LoggerFrom(ctx).Info("authenticated Talos Version() call failed", "error", versionErr.Error(), "endpoint", endpoint)
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
		// 一度desired imageへ到達した後にrollbackした場合は、自動復旧を試みずfail-closedで停止する。
		// この分岐だけはapplyTalosUpgradeを呼ばない。
		mismatchMessage = "The previously observed Talos image is no longer running; automatic rollback recovery is stopped."
		readyMessage = "The Host no longer reports the previously observed desired Talos image."
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, infrav1alpha1.ReasonRolledBack, mismatchMessage,
			metav1.ConditionFalse, infrav1alpha1.ReasonRolledBack, readyMessage,
			0)
		return result, true, err
	}

	// version/schematicがdesiredと一致しない場合、in-place updateを試みる。CAPI coreのRuntimeSDK
	// ExtensionConfig経由のUpdateMachine hookはKubeadmControlPlaneの内部実装専用であり、独自の
	// TartControlPlaneを持つこのproviderでは決して呼び出されない。そのためcontroller/runtimeextension
	// が実装済みの安全なstaged apply+reboot engine(etcd quorum gate、drain policy含む)を
	// このreconcile loopから直接呼び出す。
	outcome := r.applyTalosUpgrade(ctx, machine, version.Tag, authenticated)
	if closeErr := authenticated.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
	}
	if outcome.FailureMessage != "" {
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, mismatchReason, outcome.FailureMessage,
			metav1.ConditionFalse, mismatchReason, readyMessage,
			0)
		return result, true, err
	}
	progressMessage := outcome.RetryMessage
	if progressMessage == "" {
		progressMessage = mismatchMessage
	}
	result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
		metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
		metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
		metav1.ConditionFalse, mismatchReason, progressMessage,
		metav1.ConditionFalse, mismatchReason, readyMessage,
		talosRequeue)
	return result, true, err
}

// applyTalosUpgradeは、観測したTalos version/schematicのmismatchを実際のOS image upgrade(Talosの
// Upgrade RPC)でin-placeに解消しようと試みる。安全条件(control planeのetcd quorum、workload Podの
// drain policy)の評価はcontroller/runtimeextensionが実装済みのPerformImageUpgradeへ委譲する
// (これはversionが既にdesiredへ到達済みの場合のmachine configuration差分適用とは別の経路であり、
// そちらが使うApplyConfigurationUpdate/MachineConfigurationUpdateはここでは使わない)。
func (r *TartMachineReconciler) applyTalosUpgrade(ctx context.Context, machine *infrav1alpha1.TartMachine, observedVersion string, authenticated *talos.Client) runtimeextension.ConfigurationUpdateOutcome {
	if err := talos.ValidateUpgrade(observedVersion, machine.Spec.Image.Version); err != nil {
		return runtimeextension.ConfigurationUpdateOutcome{FailureMessage: "The requested Talos version transition is not supported; the in-place update is stopped."}
	}
	image, err := talos.InstallerImage(machine.Spec.Image.Version, machine.Spec.Image.SchematicID)
	if err != nil {
		return runtimeextension.ConfigurationUpdateOutcome{FailureMessage: "The desired Talos installer image is invalid; the in-place update is stopped."}
	}
	clusterMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return runtimeextension.ConfigurationUpdateOutcome{RetryMessage: "The owning CAPI Machine could not be observed while the Talos in-place update is being prepared."}
	}
	// PerformImageUpgradeは本来RuntimeSDK HTTPサーバーのhandler timeout(20秒)の内側で呼ばれる
	// 前提であり、この呼び出し元にも明示的なboundを与えないと、外部Talos APIが応答しない場合に
	// reconcile workerが無期限に停止しうる。
	upgradeContext, cancel := context.WithTimeout(ctx, talosReconcileTimeout*3)
	defer cancel()
	return runtimeextension.PerformImageUpgrade(upgradeContext, r.Client, clusterMachine, machine.Spec.ProviderID.String(), image, authenticated)
}

func (r *TartMachineReconciler) reconcileMaintenanceTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, endpoint string, configuration []byte) (ctrl.Result, error) {
	if machineusecase.IsProvisioned(machine) {
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
	if err := configbuilder.ValidateMachineConfiguration(effectiveConfiguration); err != nil {
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, "ConfigurationInvalid", "The complete Talos machine configuration failed client-side validation.", "The complete Talos machine configuration is invalid.")
	}
	// maintenance mode(未installのnode)ではSTAGED modeはpersisted configを書くだけでSetConfigを
	// 呼ばないため、boot sequenceがconfig完了を検知できず永久にmaintenance modeへ留まる。
	// NO_REBOOT(AUTO) modeはpersisted configに加えてSetConfigも呼ぶため、Talos自身のmaintenance
	// mode boot sequenceがconfigの完了を検知し、自動でinstallとrebootへ進む。
	if err := maintenance.ApplyConfigurationNoReboot(ctx, effectiveConfiguration); err != nil {
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
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineTalosReachableCondition, reachableStatus, reachableReason, reachableMessage, machine.Generation)
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineProvisionedCondition, provisionedStatus, provisionedReason, provisionedMessage, machine.Generation)
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineTalosUpToDateCondition, upToDateStatus, upToDateReason, upToDateMessage, machine.Generation)
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition, readyStatus, readyReason, readyMessage, machine.Generation)
	machine.Status.ObservedGeneration = machine.Generation
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *TartMachineReconciler) observedOrSelectedHost(ctx context.Context, machine *infrav1alpha1.TartMachine, hosts []infrav1alpha1.TartHost) (*infrav1alpha1.TartHost, error) {
	capiMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return nil, err
	}
	failureDomain := capiMachine.Spec.FailureDomain
	// spec.hostRefはclaim後immutableである。statusと不一致の場合はsafe-stopする。
	if machine.Status.HostRef != nil && machine.Spec.HostRef != nil && machine.Spec.HostRef.Name != "" && machine.Spec.HostRef.Name != machine.Status.HostRef.Name {
		return nil, errHostSelectionMismatch
	}
	if machine.Status.HostRef != nil {
		observed := &infrav1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKey{Name: machine.Status.HostRef.Name}, observed); err != nil {
			return nil, err
		}
		if observed.Spec.ConsumerRef == nil || observed.Spec.ConsumerRef.UID != machine.UID {
			return nil, nil
		}
		if !hostusecase.MatchesForFailureDomain(observed.Labels, observed.Spec, machine.Spec.HostSelector, failureDomain) {
			return nil, errHostSelectionMismatch
		}
		return observed, nil
	}
	for index := range hosts {
		if hosts[index].Spec.ConsumerRef != nil && hosts[index].Spec.ConsumerRef.UID == machine.UID {
			if !hostusecase.MatchesForFailureDomain(hosts[index].Labels, hosts[index].Spec, machine.Spec.HostSelector, failureDomain) {
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
		eligibility := hostusecase.Classify(selected.Spec)
		if (eligibility != hostdomain.Available && eligibility != hostdomain.Reusable) || !hostusecase.MatchesForFailureDomain(selected.Labels, selected.Spec, machine.Spec.HostSelector, failureDomain) {
			return nil, hostusecase.ErrNoEligibleHost
		}
		return selected, nil
	}
	selected, err := hostusecase.SelectFreshForFailureDomainWithRendezvous(hosts, machine.Spec.HostSelector, failureDomain, machine.UID)
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

	configuration, configurationErr := r.BootstrapConfiguration(ctx, machine)
	if configurationErr != nil && !errors.Is(configurationErr, ErrBootstrapDataUnavailable) {
		return ctrl.Result{}, configurationErr
	}
	if !machineusecase.HasShutdownRequest(machine) {
		requested, requestErr := requestHostShutdown(ctx, selected, configuration)
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
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownVerificationUnavailable, "The WakeOnLAN/Manual power backend cannot independently observe power-off state; explicit shutdown confirmation is required.", shutdownConfirmationRequeue)
		}
		if errors.Is(observationErr, ErrShutdownStateUnverifiable) && isShutdownConfirmationRequired(selected.Spec.Power.Backend) {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownVerificationUnavailable, "The WakeOnLAN/Manual power backend cannot independently observe power-off state; explicit shutdown confirmation is required.", shutdownConfirmationRequeue)
		}
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host stop state could not be verified; the Machine finalizer remains.", shutdownConfirmationRequeue)
	}
	if !stopped {
		if isShutdownConfirmationRequired(selected.Spec.Power.Backend) && !shutdownConfirmed(selected, machine) {
			return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownVerificationUnavailable, "The WakeOnLAN/Manual power backend cannot independently observe power-off state; explicit shutdown confirmation is required.", shutdownConfirmationRequeue)
		}
		return r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonShutdownUnconfirmed, "The allocated Host still responds to a Talos API; the Host claim remains.", shutdownConfirmationRequeue)
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

// capiDeletionDrainCompleteはprovider resourceの削除前にCAPI Machine controllerがdrainとvolume detachを完了したことを確認する。pre-terminate hookがあるcontrol planeでは、そのhook解除後にCAPIがinfra削除段階へ進んだことも同時に確認できる。
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

func requestHostShutdown(ctx context.Context, selected *infrav1alpha1.TartHost, configuration []byte) (bool, error) {
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
	if strings.TrimSpace(confirmation.HostID) == "" || confirmation.HostID != host.Spec.HostID {
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
	if selected.Spec.Power.Backend == infrav1alpha1.PowerBackendRedfish {
		state, err := power.RedfishPowerState(ctx, r.Client, r.ManagementNamespace, selected)
		if err != nil {
			return false, err
		}
		return state == power.PowerStateOff, nil
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
			// TLS接続自体は成功しており、hostはまだ応答している。confirmationで上書きしない。
			return false, nil
		}
		authenticatedErr = err
	}
	dialCtx, cancel := context.WithTimeout(ctx, maintenanceDialTimeout)
	maintenance, err := talos.DialMaintenance(dialCtx, endpoint)
	cancel()
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
		// Dialは成功したがInventory取得に失敗した場合も、TCP/TLSレベルでhostは応答しているため、停止証拠とはしない。
		return false, nil
	}
	// ここまでで、どのTalos APIも正の証拠として応答していない。独立observerがないため、operatorの明示的なconfirmationが必要。
	if isShutdownConfirmationRequired(selected.Spec.Power.Backend) && shutdownConfirmed(selected, machine) {
		return true, nil
	}
	if authenticatedErr != nil {
		return false, fmt.Errorf("%w: maintenance API unreachable is not proof of power off (authenticated error: %v): %w", ErrShutdownStateUnverifiable, authenticatedErr, err)
	}
	return false, fmt.Errorf("%w: maintenance API unreachable is not proof of power off: %w", ErrShutdownStateUnverifiable, err)
}

func (r *TartMachineReconciler) previousConsumerRef(ctx context.Context, machine *infrav1alpha1.TartMachine, consumer corev1.ObjectReference) (infrav1alpha1.PreviousConsumerRef, error) {
	previous := infrav1alpha1.PreviousConsumerRef{
		Namespace: consumer.Namespace,
		Name:      consumer.Name,
		UID:       consumer.UID,
	}
	capiMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		if errors.Is(err, controller.ErrCAPIMachineUnavailable) {
			// CAPI Machineがまだ存在するならClusterIDは解決できるはず。Unavailableは一時的な観測失敗として扱い、releaseをブロックする。
			return previous, fmt.Errorf("resolve previous consumer ClusterID: %w", err)
		}
		return previous, fmt.Errorf("resolve previous consumer ClusterID: %w", err)
	}
	var cluster clusterv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: capiMachine.Namespace, Name: capiMachine.Spec.ClusterName}, &cluster); err != nil {
		return previous, fmt.Errorf("resolve previous consumer ClusterID: %w", err)
	}
	previous.ClusterID = ""
	if ref := cluster.Spec.InfrastructureRef; ref.APIGroup == infrav1alpha1.GroupVersion.Group && ref.Kind == controller.TartClusterKind && ref.Name != "" {
		var tartCluster infrav1alpha1.TartCluster
		if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, &tartCluster); err != nil {
			return previous, fmt.Errorf("resolve previous consumer ClusterID: %w", err)
		}
		if tartCluster.Spec.ClusterID == "" {
			return previous, fmt.Errorf("resolve previous consumer ClusterID: TartCluster ClusterID is empty")
		}
		previous.ClusterID = tartCluster.Spec.ClusterID
	}
	if previous.ClusterID == "" {
		return previous, fmt.Errorf("resolve previous consumer ClusterID: ClusterID is empty")
	}
	return previous, nil
}

func (r *TartMachineReconciler) report(ctx context.Context, machine *infrav1alpha1.TartMachine, reason, message string) error {
	original := machine.DeepCopy()
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition, metav1.ConditionFalse, reason, message, machine.Generation)
	machine.Status.ObservedGeneration = machine.Generation
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return err
	}
	return nil
}

func (r *TartMachineReconciler) reportAndRequeue(ctx context.Context, machine *infrav1alpha1.TartMachine, reason, message string, after time.Duration) (ctrl.Result, error) {
	err := r.report(ctx, machine, reason, message)
	return ctrl.Result{RequeueAfter: after}, err
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
