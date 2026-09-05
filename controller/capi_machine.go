package controller

import (
	"context"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

var errCAPIMachineUnavailable = errors.New("CAPI Machine for provider resource is unavailable")

func findCAPIMachineForInfrastructure(ctx context.Context, c client.Client, object client.Object) (*clusterv1.Machine, error) {
	return findCAPIMachine(ctx, c, object, func(machine *clusterv1.Machine) bool {
		ref := machine.Spec.InfrastructureRef
		return ref.APIGroup == infrav1alpha1.GroupVersion.Group && ref.Kind == tartMachineKind && ref.Name == object.GetName()
	})
}

func findCAPIMachineForBootstrap(ctx context.Context, c client.Client, object client.Object) (*clusterv1.Machine, error) {
	return findCAPIMachine(ctx, c, object, func(machine *clusterv1.Machine) bool {
		ref := machine.Spec.Bootstrap.ConfigRef
		return ref.APIGroup == bootstrapv1alpha1.GroupVersion.Group && ref.Kind == tartBootstrapConfigKind && ref.Name == object.GetName()
	})
}

func findCAPIMachine(ctx context.Context, c client.Client, object client.Object, matches func(*clusterv1.Machine) bool) (*clusterv1.Machine, error) {
	for _, owner := range object.GetOwnerReferences() {
		if owner.APIVersion != clusterv1.GroupVersion.String() || owner.Kind != capiMachineKind {
			continue
		}

		machine := &clusterv1.Machine{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: owner.Name}, machine); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, errCAPIMachineUnavailable
			}
			return nil, err
		}
		if owner.UID != "" && machine.UID != owner.UID {
			return nil, errCAPIMachineUnavailable
		}
		if !matches(machine) {
			return nil, errors.New("CAPI Machine reference does not match provider resource")
		}
		return machine, nil
	}

	machines := &clusterv1.MachineList{}
	if err := c.List(ctx, machines, client.InNamespace(object.GetNamespace())); err != nil {
		if runtime.IsNotRegisteredError(err) {
			return nil, errCAPIMachineUnavailable
		}
		return nil, err
	}
	var matched *clusterv1.Machine
	for index := range machines.Items {
		candidate := &machines.Items[index]
		if !matches(candidate) {
			continue
		}
		if matched != nil {
			return nil, errors.New("multiple CAPI Machines reference provider resource")
		}
		matched = candidate
	}
	if matched == nil {
		return nil, errCAPIMachineUnavailable
	}
	return matched, nil
}
