package tartmachine

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubernetesadapter "github.com/walnuts1018/cluster-api-provider-tart/adapter/kubernetes"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
)

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

	if selected.Spec.HostID.IsZero() {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost has no persistent identity yet.")
	}
	providerID, err := hostdomain.NewProviderID(selected.Spec.HostID)
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
	// この再取得が失敗した場合にFailureDomainを「制約なし」として扱うとfail-closedの意図が崩れるため、
	// 検証できないままclaimへ進まずrequeueまたはerrorで停止する。
	capiMachineForClaim, capiClaimErr := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if capiClaimErr != nil {
		if errors.Is(capiClaimErr, controller.ErrCAPIMachineUnavailable) {
			result, reportErr := r.reportAndRequeue(ctx, machine, infrav1alpha1.ReasonHostMismatch, "The corresponding CAPI Machine is not available to re-validate Host claim placement.", 5*time.Second)
			return nil, result, true, reportErr
		}
		return nil, ctrl.Result{}, false, capiClaimErr
	}
	if capiMachineForClaim == nil {
		return nil, ctrl.Result{}, false, errors.New("the corresponding CAPI Machine could not be resolved for Host claim validation")
	}
	failureDomainForClaim := capiMachineForClaim.Spec.FailureDomain
	claimReq := hostusecase.ClaimRequest{
		Consumer:       consumer,
		ExpectedHostID: selected.Spec.HostID,
		Selector:       machine.Spec.HostSelector,
		FailureDomain:  failureDomainForClaim,
		Mode:           claimModeForSelectedHost(machine, selected),
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
	if selected.Spec.HostID.IsZero() {
		return nil, ctrl.Result{}, true, r.report(ctx, machine, "HostIdentityInvalid", "The claimed Host has an invalid HostID; ProviderID publication is stopped.")
	}
	freshProviderID, err := hostdomain.NewProviderID(selected.Spec.HostID)
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

func claimModeForSelectedHost(machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost) hostusecase.ClaimMode {
	if machine.Spec.HostRef != nil && machine.Spec.HostRef.Name == selected.Name && hostusecase.Classify(selected.Spec) == hostdomain.Reusable {
		return hostusecase.ClaimExplicitReusable
	}
	return hostusecase.ClaimFreshAutomatic
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
	configuration, err := BootstrapConfiguration(ctx, r.Client, machine)
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

// BootstrapConfigurationはCAPI Machineの参照からBootstrap Secretを解決し、割当済みProviderIDを
// 適用したeffective machine configurationを返す。Client以外のreconciler stateに依存しないため、
// tartcontrolplaneなど他のcontrollerからも呼び出せるpackage-level関数として提供する。
func BootstrapConfiguration(ctx context.Context, c client.Client, machine *infrav1alpha1.TartMachine) ([]byte, error) {
	clusterMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, c, machine)
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
	if err := c.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, config); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrBootstrapDataUnavailable
		}
		return nil, err
	}
	if strings.TrimSpace(config.Status.DataSecretName) == "" {
		return nil, ErrBootstrapDataUnavailable
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: config.Status.DataSecretName}, secret); err != nil {
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
	effectiveConfiguration := bytes.Clone(configuration)
	if !machine.Spec.ProviderID.IsZero() {
		effectiveConfiguration, err = talos.SetProviderID(effectiveConfiguration, machine.Spec.ProviderID.String())
		if err != nil {
			return nil, fmt.Errorf("set allocated ProviderID on bootstrap configuration: %w", err)
		}
	}
	return effectiveConfiguration, nil
}
