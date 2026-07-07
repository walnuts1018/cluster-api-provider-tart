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

package inplaceupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

// ManifestResolverはdigest固定OCI参照から署名検証済みManifestを解決する。
type ManifestResolver interface {
	ResolveManifest(context.Context, string) (artifact.ValidatedManifest, error)
}

// WorkflowStarterはUpdate OperationとPlanを開始するapplication境界である。
type WorkflowStarter interface {
	Start(
		context.Context,
		application.WorkflowInput,
	) (*infrastructurev1beta1.TartHostOperation, error)
}

// ServiceはUpdateMachine requestをlive Kubernetes stateへ接続する。
type Service struct {
	client    client.Client
	manifests ManifestResolver
	workflow  WorkflowStarter
	now       func() time.Time
}

// NewServiceはUpdateMachine用Kubernetes adapterを生成する。
func NewService(
	k8sClient client.Client,
	manifests ManifestResolver,
	workflow WorkflowStarter,
) *Service {
	return &Service{
		client:    k8sClient,
		manifests: manifests,
		workflow:  workflow,
		now:       time.Now,
	}
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines;tarthosts,verbs=get

// Startはdesired specとlive statusを結合し、OSOnly Updateを開始する。
func (service *Service) Start(
	ctx context.Context,
	request *runtimehooksv1.UpdateMachineRequest,
) (*infrastructurev1beta1.TartHostOperation, error) {
	if request == nil {
		return nil, fmt.Errorf("UpdateMachine request is required")
	}
	desired, err := decodeTartMachine(request.Desired.InfrastructureMachine)
	if err != nil {
		return nil, err
	}
	if desired.Name == "" || desired.Namespace == "" {
		return nil, fmt.Errorf("desired TartMachine name and namespace are required")
	}

	live := &infrastructurev1beta1.TartMachine{}
	if err := service.client.Get(ctx, client.ObjectKeyFromObject(&desired), live); err != nil {
		return nil, fmt.Errorf("get live TartMachine: %w", err)
	}
	live.Spec = desired.Spec
	if live.Status.HostRef == nil {
		return nil, fmt.Errorf("live TartMachine hostRef is required")
	}
	host := &infrastructurev1beta1.TartHost{}
	if err := service.client.Get(ctx, client.ObjectKey{
		Namespace: live.Status.HostRef.Namespace,
		Name:      live.Status.HostRef.Name,
	}, host); err != nil {
		return nil, fmt.Errorf("get TartHost for UpdateMachine: %w", err)
	}
	manifest, err := service.manifests.ResolveManifest(ctx, live.Spec.Image.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolve target OS Artifact Manifest: %w", err)
	}
	value := manifest.Value()
	machine := request.Desired.Machine.DeepCopy()
	return service.workflow.Start(ctx, application.WorkflowInput{
		StartInput: application.StartInput{
			Machine:                  machine,
			TartMachine:              live,
			BootstrapConfig:          request.Desired.BootstrapConfig,
			Host:                     host,
			TargetImageDigest:        value.Image.Digest,
			TargetArtifactGeneration: value.Generation,
			Now:                      service.now().UTC(),
		},
		Manifest: manifest,
	})
}

func decodeTartMachine(extension runtime.RawExtension) (infrastructurev1beta1.TartMachine, error) {
	raw := extension.Raw
	if len(raw) == 0 && extension.Object != nil {
		var err error
		raw, err = json.Marshal(extension.Object)
		if err != nil {
			return infrastructurev1beta1.TartMachine{}, fmt.Errorf("encode desired TartMachine: %w", err)
		}
	}
	if len(raw) == 0 {
		return infrastructurev1beta1.TartMachine{}, fmt.Errorf("desired TartMachine is required")
	}
	var machine infrastructurev1beta1.TartMachine
	if err := json.Unmarshal(raw, &machine); err != nil {
		return infrastructurev1beta1.TartMachine{}, fmt.Errorf("decode desired TartMachine: %w", err)
	}
	return machine, nil
}
