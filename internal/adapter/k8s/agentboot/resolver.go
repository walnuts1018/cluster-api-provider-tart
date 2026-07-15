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

package agentboot

import (
	"context"
	"fmt"
	"net"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentbootdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentboot"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentartifact"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrNotFound    = agentbootdomain.ErrTargetNotFound
	ErrAmbiguous   = agentbootdomain.ErrTargetAmbiguous
	ErrUnsupported = agentbootdomain.ErrUnsupportedHost
)

type Target = agentbootdomain.Target

type Resolver struct {
	client client.Client
}

func NewResolver(k8sClient client.Client) *Resolver {
	return &Resolver{client: k8sClient}
}

func (resolver *Resolver) Resolve(ctx context.Context, bootMACAddress string) (Target, error) {
	normalizedMAC, err := normalizeMAC(bootMACAddress)
	if err != nil {
		return Target{}, ErrNotFound
	}
	host, err := resolver.findHost(ctx, normalizedMAC)
	if err != nil {
		return Target{}, err
	}
	if host.Spec.Architecture != infrastructurev1beta1.ArchitectureAMD64 ||
		host.Spec.Firmware != infrastructurev1beta1.FirmwareUEFI ||
		host.Spec.PlatformProfile != agentartifact.PlatformProfileAMD64UEFIABV1 ||
		host.Spec.Management.BootDriver != "ipxe" {
		return Target{}, ErrUnsupported
	}
	operation, err := resolver.findOperation(ctx, host)
	if err != nil {
		return Target{}, err
	}
	return Target{
		HostUID:         string(host.UID),
		OperationUID:    operation.Spec.OperationID,
		BootMACAddress:  normalizedMAC,
		PlatformProfile: host.Spec.PlatformProfile,
	}, nil
}

func (resolver *Resolver) findHost(
	ctx context.Context,
	normalizedMAC string,
) (*infrastructurev1beta1.TartHost, error) {
	hosts := &infrastructurev1beta1.TartHostList{}
	if err := resolver.client.List(ctx, hosts); err != nil {
		return nil, fmt.Errorf("list TartHosts for Agent boot: %w", err)
	}
	var found *infrastructurev1beta1.TartHost
	for i := range hosts.Items {
		candidateMAC, err := normalizeMAC(hosts.Items[i].Spec.Identifiers.BootMACAddress)
		if err != nil || candidateMAC != normalizedMAC {
			continue
		}
		if found != nil {
			return nil, ErrAmbiguous
		}
		found = hosts.Items[i].DeepCopy()
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

func (resolver *Resolver) findOperation(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operations := &infrastructurev1beta1.TartHostOperationList{}
	if err := resolver.client.List(ctx, operations, client.InNamespace(host.Namespace)); err != nil {
		return nil, fmt.Errorf("list TartHostOperations for Agent boot: %w", err)
	}
	var found *infrastructurev1beta1.TartHostOperation
	for i := range operations.Items {
		operation := &operations.Items[i]
		if operation.Spec.HostRef.UID != host.UID || !agentBootPhase(operation.Status.Phase) {
			continue
		}
		if found != nil {
			return nil, ErrAmbiguous
		}
		found = operation.DeepCopy()
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

func agentBootPhase(phase infrastructurev1beta1.TartHostOperationPhase) bool {
	switch phase {
	case infrastructurev1beta1.TartHostOperationPhasePreparingBoot,
		infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent,
		infrastructurev1beta1.TartHostOperationPhaseWriting,
		infrastructurev1beta1.TartHostOperationPhaseVerifying:
		return true
	case infrastructurev1beta1.TartHostOperationPhasePending,
		infrastructurev1beta1.TartHostOperationPhaseBootTrial,
		infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
		infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		infrastructurev1beta1.TartHostOperationPhaseRollingBack,
		infrastructurev1beta1.TartHostOperationPhaseSucceeded,
		infrastructurev1beta1.TartHostOperationPhaseFailed,
		infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired:
		return false
	default:
		return false
	}
}

func normalizeMAC(value string) (string, error) {
	mac, err := net.ParseMAC(value)
	if err != nil {
		return "", err
	}
	return mac.String(), nil
}
