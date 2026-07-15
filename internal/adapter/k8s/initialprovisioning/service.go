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

package initialprovisioning

import (
	"context"
	"fmt"

	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appinitialprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

// ManifestResolver は digest 固定 OCI 参照から署名検証済み Manifest を解決する。
type ManifestResolver interface {
	ResolveManifest(context.Context, string) (artifact.ValidatedManifest, error)
}

// WorkflowStarter は初期 Provisioning workflow の application 境界である。
type WorkflowStarter interface {
	Start(context.Context, appinitialprovisioning.WorkflowInput) (appinitialprovisioning.StartResult, error)
	CompleteProvisioning(context.Context, *infrastructurev1beta1.TartHost, *infrastructurev1beta1.TartHostOperation) error
}

// Service は live Kubernetes state と初期 Provisioning workflow を接続する。
type Service struct {
	client    client.Client
	manifests ManifestResolver
	workflow  WorkflowStarter
}

func NewService(
	k8sClient client.Client,
	manifests ManifestResolver,
	workflow WorkflowStarter,
) *Service {
	return &Service{
		client:    k8sClient,
		manifests: manifests,
		workflow:  workflow,
	}
}

// Start は owner Machine と OS Artifact Manifest を解決して Provision workflow へ渡す。
func (service *Service) Start(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (appinitialprovisioning.StartResult, error) {
	if machine == nil {
		return nil, fmt.Errorf("TartMachine is required")
	}
	ownerMachine, err := util.GetOwnerMachine(ctx, service.client, machine.ObjectMeta)
	if err != nil {
		return nil, fmt.Errorf("get owner Machine: %w", err)
	}
	if ownerMachine == nil {
		return nil, fmt.Errorf("owner Machine is required")
	}
	manifest, err := service.manifests.ResolveManifest(ctx, machine.Spec.Image.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolve target OS Artifact Manifest: %w", err)
	}
	return service.workflow.Start(ctx, appinitialprovisioning.WorkflowInput{
		Machine:    machine,
		MachineUID: string(ownerMachine.UID),
		Manifest:   manifest,
	})
}

func (service *Service) CompleteProvisioning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	return service.workflow.CompleteProvisioning(ctx, host, operation)
}
