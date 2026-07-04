package allocation

import (
	"context"
	"errors"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

var ErrNoMatchingHost = errors.New("no matching TartHost")
var ErrAllocationConflict = errors.New("TartHost allocation conflict")

type ReferenceResult string

const (
	ReferenceMissing    ReferenceResult = "Missing"
	ReferenceConsistent ReferenceResult = "Consistent"
	ReferenceRepaired   ReferenceResult = "Repaired"
)

type Service struct {
	client client.Client
}

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
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
	phase, err := hostdomain.ParsePhase(string(host.Status.Phase))
	if err != nil {
		return false, nil
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

	candidate := allocationdomain.Candidate{
		Phase:             phase,
		Assigned:          host.Spec.ConsumerRef != nil,
		Architecture:      string(host.Spec.Architecture),
		Firmware:          string(host.Spec.Firmware),
		PlatformProfile:   host.Spec.PlatformProfile,
		RootDiskSizeBytes: host.Status.Inventory.RootDisk.SizeBytes,
		Capabilities:      capabilities,
		Labels:            host.Labels,
	}
	return allocationdomain.Match(requirements, candidate) == allocationdomain.MismatchNone, nil
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
