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

package machineexecution

import (
	"context"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

const placeholderPlanDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

type HostReferenceService interface {
	EnsureMachineHostReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (allocationdomain.ReferenceResult, error)
}

type ProvisionWorkflow interface {
	Start(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		planDigest string,
	) (appprovisioning.StartResult, error)
	CompleteProvisioning(
		ctx context.Context,
		host *infrastructurev1beta1.TartHost,
		operation *infrastructurev1beta1.TartHostOperation,
	) error
}

type NodeHealthObserver interface {
	Observe(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machinehealthdomain.NodeObservation, bool, error)
}

type Workflow struct {
	client.Client
	HostReferences HostReferenceService
	NodeHealth     NodeHealthObserver
	Provisioner    ProvisionWorkflow
	Recorder       record.EventRecorder
}

func NewWorkflow(
	k8sClient client.Client,
	hostReferences HostReferenceService,
	nodeHealth NodeHealthObserver,
	provisioner ProvisionWorkflow,
	recorder record.EventRecorder,
) *Workflow {
	return &Workflow{
		Client:         k8sClient,
		HostReferences: hostReferences,
		NodeHealth:     nodeHealth,
		Provisioner:    provisioner,
		Recorder:       recorder,
	}
}
