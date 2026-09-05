package controller

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/host"
)

const (
	tartMachineFinalizer        = "tart.cluster.x-k8s.io/machine-lifecycle"
	shutdownConfirmationRequeue = 30 * time.Second
)

// TartMachineReconcilerはHost claimとProviderIDの確立を担当する。Talosへの副作用は
// まだUpdate Extensionへ委譲する段階なので、初回provisioning後のoperationは開始しない。
type TartMachineReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch

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

	if selected.Spec.ID == "" {
		return r.report(ctx, &machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost has no persistent identity yet.")
	}
	providerID, err := host.ProviderID(selected.Spec.ID)
	if err != nil {
		return r.report(ctx, &machine, infrav1alpha1.ReasonHostIDUnavailable, "The selected TartHost identity is invalid.")
	}
	if machine.Spec.ProviderID != "" && machine.Spec.ProviderID != providerID {
		return r.report(ctx, &machine, infrav1alpha1.ReasonHostMismatch, "The existing ProviderID does not match the allocated TartHost identity.")
	}

	consumer := corev1.ObjectReference{
		APIVersion: infrav1alpha1.GroupVersion.String(),
		Kind:       "TartMachine",
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

	statusOriginal := machine.DeepCopy()
	machine.Status.HostRef = &corev1.LocalObjectReference{Name: selected.Name}
	setCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition, metav1.ConditionFalse, infrav1alpha1.ReasonNotImplemented, "Host allocation and ProviderID are established; Talos provisioning is not implemented yet.", machine.Generation)
	machine.Status.ObservedGeneration = machine.Generation
	if err := r.Status().Patch(ctx, &machine, client.MergeFrom(statusOriginal)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
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
