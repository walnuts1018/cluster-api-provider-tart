package step

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	resourcefinalizerdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/resourcefinalizer"
)

type Executor struct {
	client       client.Client
	name         resourcefinalizerdomain.Name
	resourceName string
}

func NewExecutor(k8sClient client.Client, name resourcefinalizerdomain.Name, resource string) *Executor {
	return &Executor{client: k8sClient, name: name, resourceName: resource}
}

func (executor *Executor) Apply(
	ctx context.Context,
	object client.Object,
	desired resourcefinalizerdomain.DesiredState,
) (resourcefinalizerdomain.Result, error) {
	command, err := resourcefinalizerdomain.Decide(desired, executor.observe(object))
	if err != nil {
		return nil, err
	}
	switch command.(type) {
	case resourcefinalizerdomain.CommandAdd:
		return resourcefinalizerdomain.ResultPatched{}, executor.patch(ctx, object, controllerutil.AddFinalizer)
	case resourcefinalizerdomain.CommandRemove:
		return resourcefinalizerdomain.ResultPatched{}, executor.patch(ctx, object, controllerutil.RemoveFinalizer)
	case resourcefinalizerdomain.CommandNoop:
		return resourcefinalizerdomain.ResultUnchanged{}, nil
	default:
		return nil, fmt.Errorf("unknown resource finalizer command: %T", command)
	}
}

func (executor *Executor) Present(object client.Object) bool {
	return controllerutil.ContainsFinalizer(object, executor.finalizer())
}

func (executor *Executor) observe(object client.Object) resourcefinalizerdomain.ObservedState {
	if executor.Present(object) {
		return resourcefinalizerdomain.ObservedPresent{}
	}
	return resourcefinalizerdomain.ObservedAbsent{}
}

func (executor *Executor) patch(
	ctx context.Context,
	object client.Object,
	transition func(client.Object, string) bool,
) error {
	original := object.DeepCopyObject().(client.Object)
	transition(object, executor.finalizer())
	if err := executor.client.Patch(ctx, object, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("patch %s finalizer: %w", executor.resource(), err)
	}
	return nil
}

func (executor *Executor) finalizer() string {
	return executor.name.String()
}

func (executor *Executor) resource() string {
	if executor.resourceName == "" {
		return "resource"
	}
	return executor.resourceName
}
