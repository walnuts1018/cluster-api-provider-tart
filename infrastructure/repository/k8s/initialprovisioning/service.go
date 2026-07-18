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
	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
	completeprovisioning "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/complete_provisioning"
	appinitialprovisioning "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/provision_machine"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
	"github.com/walnuts1018/cluster-api-provider-tart/infrastructure/workflowresult"
)

// ManifestResolver は digest 固定 OCI 参照から署名検証済み Manifest を解決する。
type ManifestResolver interface {
	ResolveManifest(context.Context, string) (artifact.ValidatedManifest, error)
}

// WorkflowStarter は初期 Provisioning workflow の application 境界である。
type WorkflowStarter interface {
	Do(context.Context, appinitialprovisioning.Command) sharedresult.Result[appinitialprovisioning.Event, sharedworkflow.Failure]
}

type WorkflowCompleter interface {
	Do(context.Context, completeprovisioning.Command) sharedresult.Result[completeprovisioning.Event, sharedworkflow.Failure]
}

// Service は live Kubernetes state と初期 Provisioning workflow を接続する。
type Service struct {
	client    client.Client
	manifests ManifestResolver
	starter   WorkflowStarter
	completer WorkflowCompleter
}

func NewService(
	k8sClient client.Client,
	manifests ManifestResolver,
	starter WorkflowStarter,
	completer WorkflowCompleter,
) *Service {
	return &Service{
		client:    k8sClient,
		manifests: manifests,
		starter:   starter,
		completer: completer,
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
	event, err := workflowresult.Unwrap(service.starter.Do(ctx, appinitialprovisioning.Command{
		Machine:    machine,
		MachineUID: string(ownerMachine.UID),
		Manifest:   manifest,
	}))
	if err != nil {
		return nil, err
	}
	switch event := event.(type) {
	case appinitialprovisioning.MachineProvisioningStarted:
		return event.Result, nil
	case appinitialprovisioning.HostAllocationPending:
		return event.Result, nil
	default:
		panic(fmt.Sprintf("unknown provisioning event: %T", event))
	}
}

func (service *Service) CompleteProvisioning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	_, err := workflowresult.Unwrap(service.completer.Do(ctx, completeprovisioning.Command{Host: host, Operation: operation}))
	return err
}
