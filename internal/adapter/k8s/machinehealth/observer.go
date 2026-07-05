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

package machinehealth

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

const remoteClientSource = "tartmachine-v1beta1-controller"

type workloadClientFactory func(
	context.Context,
	string,
	client.Client,
	client.ObjectKey,
) (client.Client, error)

type Observer struct {
	managementClient  client.Client
	newWorkloadClient workloadClientFactory
}

func NewObserver(managementClient client.Client) *Observer {
	return &Observer{
		managementClient:  managementClient,
		newWorkloadClient: remote.NewClusterClient,
	}
}

func (o *Observer) Observe(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (machinehealthdomain.NodeObservation, bool, error) {
	coreMachine, err := util.GetOwnerMachine(ctx, o.managementClient, machine.ObjectMeta)
	if err != nil {
		return machinehealthdomain.NodeObservation{}, false, fmt.Errorf("get owner Machine: %w", err)
	}
	if coreMachine == nil || !coreMachine.Status.NodeRef.IsDefined() {
		return machinehealthdomain.NodeObservation{}, false, nil
	}
	if coreMachine.Spec.ClusterName == "" {
		return machinehealthdomain.NodeObservation{}, false, fmt.Errorf(
			"owner Machine %s/%s has no clusterName",
			coreMachine.Namespace,
			coreMachine.Name,
		)
	}

	clusterKey := client.ObjectKey{Namespace: coreMachine.Namespace, Name: coreMachine.Spec.ClusterName}
	workloadClient, err := o.newWorkloadClient(ctx, remoteClientSource, o.managementClient, clusterKey)
	if err != nil {
		return machinehealthdomain.NodeObservation{}, false, fmt.Errorf(
			"create workload client for Cluster %s/%s: %w",
			clusterKey.Namespace,
			clusterKey.Name,
			err,
		)
	}

	node := &corev1.Node{}
	if err := workloadClient.Get(ctx, types.NamespacedName{Name: coreMachine.Status.NodeRef.Name}, node); err != nil {
		return machinehealthdomain.NodeObservation{}, false, fmt.Errorf(
			"get workload Node %s: %w",
			coreMachine.Status.NodeRef.Name,
			err,
		)
	}

	return machinehealthdomain.NodeObservation{
		MachineProviderID: machine.Spec.ProviderID,
		NodeProviderID:    node.Spec.ProviderID,
		NodeReady:         nodeReady(node),
		ExpectedVersion:   coreMachine.Spec.Version,
		NodeVersion:       node.Status.NodeInfo.KubeletVersion,
	}, true, nil
}

func nodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
