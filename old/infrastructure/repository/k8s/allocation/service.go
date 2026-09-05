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

package allocation

import (
	"context"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	domainhostallocation "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/hostallocation"
	applicationhostallocation "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/allocate_host"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/allocation"
	capability "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/host"
)

var ErrNoMatchingHost = allocationdomain.ErrNoMatchingHost
var ErrAllocationConflict = allocationdomain.ErrConflict

type ReferenceResult = allocationdomain.ReferenceResult

const (
	ReferenceMissing    = allocationdomain.ReferenceMissing
	ReferenceConsistent = allocationdomain.ReferenceConsistent
	ReferenceRepaired   = allocationdomain.ReferenceRepaired
)

type Service struct {
	client client.Client
}

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
}

func (s *Service) ListCandidates(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) ([]domainhostallocation.Candidate, error) {
	var hosts infrastructurev1beta1.TartHostList
	if err := s.client.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return nil, fmt.Errorf("list TartHosts: %w", err)
	}
	sort.Slice(hosts.Items, func(i, j int) bool {
		return hosts.Items[i].Name < hosts.Items[j].Name
	})

	candidates := make([]domainhostallocation.Candidate, 0, len(hosts.Items))
	for i := range hosts.Items {
		candidate, err := candidateForAllocation(&hosts.Items[i])
		if err != nil {
			return nil, fmt.Errorf("map TartHost %s/%s: %w", hosts.Items[i].Namespace, hosts.Items[i].Name, err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (s *Service) ReserveCandidate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host domainhostallocation.HostRef,
) (applicationhostallocation.ReservationResult, error) {
	current := &infrastructurev1beta1.TartHost{}
	key := client.ObjectKey{Namespace: host.Namespace, Name: host.Name}
	if err := s.client.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return applicationhostallocation.RetrySelection{}, nil
		}
		return nil, fmt.Errorf("get TartHost %s/%s: %w", host.Namespace, host.Name, err)
	}
	if current.Spec.ConsumerRef != nil {
		if consumerMatchesMachine(current.Spec.ConsumerRef, machine) {
			return applicationhostallocation.Reserved{Host: current}, nil
		}
		return applicationhostallocation.RetrySelection{}, nil
	}

	current.Spec.ConsumerRef = &infrastructurev1beta1.ResourceReference{
		Namespace: machine.Namespace,
		Name:      machine.Name,
		UID:       machine.UID,
	}
	if err := s.client.Update(ctx, current); err != nil {
		if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
			return applicationhostallocation.RetrySelection{}, nil
		}
		return nil, fmt.Errorf("reserve TartHost %s/%s: %w", current.Namespace, current.Name, err)
	}
	return applicationhostallocation.Reserved{Host: current}, nil
}

func (s *Service) Reserve(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	requirements allocationdomain.Requirements,
) (*infrastructurev1beta1.TartHost, error) {
	var hosts infrastructurev1beta1.TartHostList
	if err := s.client.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return nil, fmt.Errorf("list TartHosts: %w", err)
	}
	sort.Slice(hosts.Items, func(i, j int) bool {
		return hosts.Items[i].Name < hosts.Items[j].Name
	})

	for i := range hosts.Items {
		candidate := &hosts.Items[i]
		if consumerMatchesMachine(candidate.Spec.ConsumerRef, machine) {
			matches, err := matchesClaimedRequirements(requirements, candidate)
			if err != nil {
				return nil, fmt.Errorf("evaluate claimed TartHost %s/%s: %w", candidate.Namespace, candidate.Name, err)
			}
			if matches {
				return candidate.DeepCopy(), nil
			}
			continue
		}
		matches, err := matchesRequirements(requirements, candidate)
		if err != nil {
			return nil, fmt.Errorf("evaluate TartHost %s/%s: %w", candidate.Namespace, candidate.Name, err)
		}
		if !matches {
			continue
		}

		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(candidate), current); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("get TartHost %s/%s: %w", candidate.Namespace, candidate.Name, err)
		}
		matches, err = matchesRequirements(requirements, current)
		if err != nil {
			return nil, fmt.Errorf("re-evaluate TartHost %s/%s: %w", current.Namespace, current.Name, err)
		}
		if !matches {
			continue
		}

		current.Spec.ConsumerRef = &infrastructurev1beta1.ResourceReference{
			Namespace: machine.Namespace,
			Name:      machine.Name,
			UID:       machine.UID,
		}
		if err := s.client.Update(ctx, current); err != nil {
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("reserve TartHost %s/%s: %w", current.Namespace, current.Name, err)
		}
		return current, nil
	}

	return nil, ErrNoMatchingHost
}

func (s *Service) EnsureMachineHostReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (ReferenceResult, error) {
	if machine.Status.HostRef != nil {
		host := &infrastructurev1beta1.TartHost{}
		key := client.ObjectKey{
			Namespace: machine.Status.HostRef.Namespace,
			Name:      machine.Status.HostRef.Name,
		}
		if err := s.client.Get(ctx, key, host); err != nil {
			if apierrors.IsNotFound(err) {
				return "", fmt.Errorf("%w: referenced TartHost %s/%s does not exist", ErrAllocationConflict, key.Namespace, key.Name)
			}
			return "", fmt.Errorf("get referenced TartHost %s/%s: %w", key.Namespace, key.Name, err)
		}
		if host.UID != machine.Status.HostRef.UID || !consumerMatchesMachine(host.Spec.ConsumerRef, machine) {
			return "", fmt.Errorf(
				"%w: TartMachine %s/%s and TartHost %s/%s references do not match",
				ErrAllocationConflict,
				machine.Namespace,
				machine.Name,
				host.Namespace,
				host.Name,
			)
		}
		return ReferenceConsistent, nil
	}

	var hosts infrastructurev1beta1.TartHostList
	if err := s.client.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return "", fmt.Errorf("list TartHosts for reference repair: %w", err)
	}
	var claimedHost *infrastructurev1beta1.TartHost
	for i := range hosts.Items {
		host := &hosts.Items[i]
		if !consumerMatchesMachine(host.Spec.ConsumerRef, machine) {
			continue
		}
		if claimedHost != nil {
			return "", fmt.Errorf(
				"%w: multiple TartHosts claim TartMachine %s/%s",
				ErrAllocationConflict,
				machine.Namespace,
				machine.Name,
			)
		}
		claimedHost = host
	}
	if claimedHost == nil {
		return ReferenceMissing, nil
	}

	original := machine.DeepCopy()
	machine.Status.HostRef = &infrastructurev1beta1.ResourceReference{
		Namespace: claimedHost.Namespace,
		Name:      claimedHost.Name,
		UID:       claimedHost.UID,
	}
	if err := s.client.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return "", fmt.Errorf("repair TartMachine hostRef: %w", err)
	}
	return ReferenceRepaired, nil
}

func matchesRequirements(requirements allocationdomain.Requirements, host *infrastructurev1beta1.TartHost) (bool, error) {
	if host.Status.Phase == "" {
		return false, nil
	}
	phase, err := hostdomain.ParsePhase(string(host.Status.Phase))
	if err != nil {
		return false, fmt.Errorf("parse TartHost phase: %w", err)
	}
	hostCapabilities := make([]capability.Capability, 0, len(host.Status.Capabilities))
	for _, value := range host.Status.Capabilities {
		parsed, err := capability.Parse(string(value))
		if err != nil {
			return false, err
		}
		hostCapabilities = append(hostCapabilities, parsed)
	}
	capabilities, err := capability.NewSet(hostCapabilities...)
	if err != nil {
		return false, err
	}

	return allocationdomain.Match(requirements, candidateForHost(host, phase, host.Spec.ConsumerRef != nil, capabilities)) ==
		allocationdomain.MismatchNone, nil
}

func matchesClaimedRequirements(
	requirements allocationdomain.Requirements,
	host *infrastructurev1beta1.TartHost,
) (bool, error) {
	hostCapabilities := make([]capability.Capability, 0, len(host.Status.Capabilities))
	for _, value := range host.Status.Capabilities {
		parsed, err := capability.Parse(string(value))
		if err != nil {
			return false, err
		}
		hostCapabilities = append(hostCapabilities, parsed)
	}
	capabilities, err := capability.NewSet(hostCapabilities...)
	if err != nil {
		return false, err
	}
	return allocationdomain.Match(
		requirements,
		candidateForHost(host, hostdomain.PhaseAvailable, false, capabilities),
	) == allocationdomain.MismatchNone, nil
}

func candidateForHost(
	host *infrastructurev1beta1.TartHost,
	phase hostdomain.Phase,
	assigned bool,
	capabilities capability.Set,
) allocationdomain.Candidate {
	return allocationdomain.Candidate{
		Phase:             phase,
		Assigned:          assigned,
		Architecture:      string(host.Spec.Architecture),
		Firmware:          string(host.Spec.Firmware),
		PlatformProfile:   host.Spec.PlatformProfile,
		RootDiskSizeBytes: host.Status.Inventory.RootDisk.SizeBytes,
		Capabilities:      capabilities,
		Labels:            host.Labels,
	}
}

func consumerMatchesMachine(
	consumer *infrastructurev1beta1.ResourceReference,
	machine *infrastructurev1beta1.TartMachine,
) bool {
	return consumer != nil &&
		consumer.Namespace == machine.Namespace &&
		consumer.Name == machine.Name &&
		consumer.UID == machine.UID
}

func candidateForAllocation(host *infrastructurev1beta1.TartHost) (domainhostallocation.Candidate, error) {
	if host.Status.Phase == "" {
		return domainhostallocation.Candidate{}, nil
	}
	phase, err := hostdomain.ParsePhase(string(host.Status.Phase))
	if err != nil {
		return domainhostallocation.Candidate{}, fmt.Errorf("parse TartHost phase: %w", err)
	}
	capabilities, err := hostCapabilities(host)
	if err != nil {
		return domainhostallocation.Candidate{}, err
	}

	assignment := domainhostallocation.Assignment(domainhostallocation.Unassigned{})
	if host.Spec.ConsumerRef != nil {
		assignment = domainhostallocation.AssignedToMachine{
			Machine: domainhostallocation.MachineRef{
				Namespace: host.Spec.ConsumerRef.Namespace,
				Name:      host.Spec.ConsumerRef.Name,
				UID:       string(host.Spec.ConsumerRef.UID),
			},
		}
	}

	return domainhostallocation.Candidate{
		Host: domainhostallocation.HostRef{
			Namespace: host.Namespace,
			Name:      host.Name,
			UID:       string(host.UID),
		},
		Phase:             phase,
		Assignment:        assignment,
		Architecture:      string(host.Spec.Architecture),
		Firmware:          string(host.Spec.Firmware),
		PlatformProfile:   host.Spec.PlatformProfile,
		RootDiskSizeBytes: host.Status.Inventory.RootDisk.SizeBytes,
		Capabilities:      capabilities,
		Labels:            host.Labels,
	}, nil
}

func hostCapabilities(host *infrastructurev1beta1.TartHost) (capability.Set, error) {
	values := make([]capability.Capability, 0, len(host.Status.Capabilities))
	for _, value := range host.Status.Capabilities {
		parsed, err := capability.Parse(string(value))
		if err != nil {
			return capability.Set{}, err
		}
		values = append(values, parsed)
	}
	return capability.NewSet(values...)
}
