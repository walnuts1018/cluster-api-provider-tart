package host

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	applicationhost "github.com/walnuts1018/cluster-api-provider-tart/internal/application/host"
	applicationprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/provisioning"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

var _ applicationhost.Service = (*Service)(nil)
var _ applicationprovisioning.HostReader = (*Service)(nil)
var _ applicationprovisioning.HostProvisioner = (*Service)(nil)

type Service struct {
	client client.Client
}

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
}

func (s *Service) ReserveAvailable(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error) {
	var hosts infrastructurev1alpha1.TartHostList
	if err := s.client.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return nil, err
	}

	for i := range hosts.Items {
		candidate := &hosts.Items[i]
		if candidate.Status.State != infrastructurev1alpha1.TartHostStateAvailable || candidate.Status.MachineRef != nil {
			continue
		}

		original := candidate.DeepCopy()
		candidate.Status.State = infrastructurev1alpha1.TartHostStateReserved
		candidate.Status.MachineRef = hostdomain.RefForMachine(machine)
		candidate.Status.ObservedGeneration = candidate.Generation
		apimeta.SetStatusCondition(&candidate.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             "Reserved",
			Message:            fmt.Sprintf("Reserved by TartMachine %s/%s", machine.Namespace, machine.Name),
			ObservedGeneration: candidate.Generation,
		})
		if err := s.client.Status().Patch(ctx, candidate, client.MergeFrom(original)); err != nil {
			if apierrors.IsConflict(err) {
				continue
			}
			return nil, err
		}
		return candidate, nil
	}

	return nil, nil
}

func (s *Service) MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
	original := host.DeepCopy()
	host.Status.State = infrastructurev1alpha1.TartHostStateProvisioning
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Provisioning",
		Message:            "Host is provisioning a TartMachine after Wake-on-LAN",
		ObservedGeneration: host.Generation,
	})
	return s.client.Status().Patch(ctx, host, client.MergeFrom(original))
}

func (s *Service) MarkProvisioned(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
	original := host.DeepCopy()
	host.Status.State = infrastructurev1alpha1.TartHostStateProvisioned
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Provisioned",
		Message:            "Host has been provisioned successfully",
		ObservedGeneration: host.Generation,
	})
	return s.client.Status().Patch(ctx, host, client.MergeFrom(original))
}

func (s *Service) ReleaseAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	if machine.Status.HostRef == nil {
		return nil
	}

	currentHost, err := s.GetAssigned(ctx, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !hostdomain.MachineRefMatches(currentHost.Status.MachineRef, machine) {
		return nil
	}

	return s.MarkAvailable(ctx, currentHost, "Released", fmt.Sprintf("Released from TartMachine %s/%s", machine.Namespace, machine.Name))
}

func (s *Service) MarkAvailable(ctx context.Context, host *infrastructurev1alpha1.TartHost, reason, message string) error {
	original := host.DeepCopy()
	host.Status.State = infrastructurev1alpha1.TartHostStateAvailable
	host.Status.MachineRef = nil
	host.Status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: host.Generation,
	})
	return s.client.Status().Patch(ctx, host, client.MergeFrom(original))
}

func (s *Service) ReleaseMissingReference(ctx context.Context, host *infrastructurev1alpha1.TartHost) (bool, error) {
	ref := host.Status.MachineRef
	if ref == nil {
		return false, nil
	}

	var machine infrastructurev1alpha1.TartMachine
	err := s.client.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, &machine)
	if err == nil && hostdomain.MachineRefMatches(host.Status.MachineRef, &machine) {
		return false, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	if err == nil && ref.UID != "" && machine.UID != ref.UID {
		return true, s.MarkAvailable(ctx, host, "StaleMachineReference", fmt.Sprintf("Host reference to TartMachine %s/%s became stale", ref.Namespace, ref.Name))
	}

	return true, s.MarkAvailable(ctx, host, "MachineMissing", fmt.Sprintf("Released stale TartMachine reference %s/%s", ref.Namespace, ref.Name))
}

func (s *Service) GetAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error) {
	if machine.Status.HostRef == nil {
		return nil, nil
	}

	var host infrastructurev1alpha1.TartHost
	if err := s.client.Get(ctx, types.NamespacedName{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, &host); err != nil {
		return nil, err
	}
	return &host, nil
}
