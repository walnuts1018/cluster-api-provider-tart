package controller

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineallocation"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
)

type HostReferenceService interface {
	EnsureMachineHostReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (allocationdomain.ReferenceResult, error)
}

type TartMachineV1Beta1Reconciler struct {
	client.Client
	HostReferences HostReferenceService
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch

func (r *TartMachineV1Beta1Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	machine := &infrastructurev1beta1.TartMachine{}
	if err := r.Get(ctx, req.NamespacedName, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TartMachine: %w", err)
	}

	result, err := r.HostReferences.EnsureMachineHostReference(ctx, machine)
	if errors.Is(err, allocationdomain.ErrConflict) {
		original := machine.DeepCopy()
		machine.Status = applicationallocation.StatusWithAllocationConflict(machine, err.Error())
		if patchErr := r.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return ctrl.Result{}, fmt.Errorf("set TartMachine AllocationConflict condition: %w", patchErr)
		}
		log.Info("TartMachine allocation conflict detected", "machine", req.String(), "error", err.Error())
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure TartMachine host reference: %w", err)
	}
	if result == allocationdomain.ReferenceRepaired {
		log.Info("Repaired TartMachine host reference", "machine", req.String())
	}
	return ctrl.Result{}, nil
}

func (r *TartMachineV1Beta1Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("tartmachine-v1beta1").
		For(&infrastructurev1beta1.TartMachine{}).
		Complete(r)
}
