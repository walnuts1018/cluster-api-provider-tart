package resourcefinalizer

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcefinalizerdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/resourcefinalizer"
	resourcefinalizerstep "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/resource_finalizer/steps"
)

const (
	TartClusterFinalizer         = "infrastructure.cluster.x-k8s.io/tartcluster"
	TartMachineTemplateFinalizer = "infrastructure.cluster.x-k8s.io/tartmachinetemplate"
	TartMachineCleanupFinalizer  = "infrastructure.cluster.x-k8s.io/tartmachine-cleanup"
)

type Result = resourcefinalizerdomain.Result
type ResultUnchanged = resourcefinalizerdomain.ResultUnchanged
type ResultPatched = resourcefinalizerdomain.ResultPatched

type Service struct {
	steps *resourcefinalizerstep.Executor
}

func NewService(k8sClient client.Client, finalizer string, resource string) (*Service, error) {
	name, err := resourcefinalizerdomain.ParseName(finalizer)
	if err != nil {
		return nil, err
	}
	return &Service{
		steps: resourcefinalizerstep.NewExecutor(k8sClient, name, resource),
	}, nil
}

func NewTartClusterService(k8sClient client.Client) *Service {
	return mustService(k8sClient, TartClusterFinalizer, "TartCluster")
}

func NewTartMachineTemplateService(k8sClient client.Client) *Service {
	return mustService(k8sClient, TartMachineTemplateFinalizer, "TartMachineTemplate")
}

func NewTartMachineService(k8sClient client.Client) *Service {
	return mustService(k8sClient, TartMachineCleanupFinalizer, "TartMachine")
}

func mustService(k8sClient client.Client, finalizer string, resource string) *Service {
	service, err := NewService(k8sClient, finalizer, resource)
	if err != nil {
		panic(err)
	}
	return service
}

func (service *Service) Ensure(ctx context.Context, object client.Object) (Result, error) {
	return service.steps.Apply(ctx, object, resourcefinalizerdomain.DesiredPresent{})
}

func (service *Service) Release(ctx context.Context, object client.Object) (Result, error) {
	return service.steps.Apply(ctx, object, resourcefinalizerdomain.DesiredAbsent{})
}

func (service *Service) Present(object client.Object) bool {
	return service.steps.Present(object)
}
