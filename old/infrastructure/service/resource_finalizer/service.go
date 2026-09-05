// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
