package host

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

type Service interface {
	ReserveAvailable(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error)
	MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
	ReleaseAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
	MarkAvailable(ctx context.Context, host *infrastructurev1alpha1.TartHost, reason, message string) error
	ReleaseMissingReference(ctx context.Context, host *infrastructurev1alpha1.TartHost) (bool, error)
}

type service struct {
	client client.Client
}

func NewService(k8sClient client.Client) Service {
	return &service{client: k8sClient}
}

func (s *service) ReserveAvailable(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error) {
	var hosts infrastructurev1alpha1.TartHostList
	if err := s.client.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return nil, err
	}

	for i := range hosts.Items {
		candidate := &hosts.Items[i]
		if candidate.Status.State != infrastructurev1alpha1.TartHostStateAvailable || candidate.Status.MachineRef != nil {
			continue
		}

		host := &infrastructurev1alpha1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(candidate), host); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if host.Status.State != infrastructurev1alpha1.TartHostStateAvailable || host.Status.MachineRef != nil {
			continue
		}

		original := host.DeepCopy()
		host.Status.State = infrastructurev1alpha1.TartHostStateReserved
		host.Status.MachineRef = RefForMachine(machine)
		host.Status.ObservedGeneration = host.Generation
		apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             "Reserved",
			Message:            fmt.Sprintf("Reserved by TartMachine %s/%s", machine.Namespace, machine.Name),
			ObservedGeneration: host.Generation,
		})
		if err := s.client.Status().Patch(ctx, host, client.MergeFrom(original)); err != nil {
			if apierrors.IsConflict(err) {
				continue
			}
			return nil, err
		}
		return host, nil
	}

	return nil, nil
}

func (s *service) MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
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

func (s *service) ReleaseAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	if machine.Status.HostRef == nil {
		return nil
	}

	var currentHost infrastructurev1alpha1.TartHost
	if err := s.client.Get(ctx, types.NamespacedName{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, &currentHost); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !MachineRefMatches(currentHost.Status.MachineRef, machine) {
		return nil
	}

	return s.MarkAvailable(ctx, &currentHost, "Released", fmt.Sprintf("Released from TartMachine %s/%s", machine.Namespace, machine.Name))
}

func (s *service) MarkAvailable(ctx context.Context, host *infrastructurev1alpha1.TartHost, reason, message string) error {
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

func (s *service) ReleaseMissingReference(ctx context.Context, host *infrastructurev1alpha1.TartHost) (bool, error) {
	ref := host.Status.MachineRef
	if ref == nil {
		return false, nil
	}

	var machine infrastructurev1alpha1.TartMachine
	err := s.client.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, &machine)
	if err == nil && MachineRefMatches(host.Status.MachineRef, &machine) {
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

func BootMACAddress(host *infrastructurev1alpha1.TartHost) string {
	if host.Spec.BootMACAddress != "" {
		return host.Spec.BootMACAddress
	}
	return host.Spec.MACAddress
}

func RefForHost(host *infrastructurev1alpha1.TartHost) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartHost",
		Namespace:  host.Namespace,
		Name:       host.Name,
		UID:        host.UID,
	}
}

func RefForMachine(machine *infrastructurev1alpha1.TartMachine) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartMachine",
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
}

func MachineRefMatches(ref *corev1.ObjectReference, machine *infrastructurev1alpha1.TartMachine) bool {
	if ref == nil {
		return false
	}
	if ref.Name != machine.Name || ref.Namespace != machine.Namespace {
		return false
	}
	return ref.UID == "" || machine.UID == "" || ref.UID == machine.UID
}

func MachineRefIndexValue(ref *corev1.ObjectReference) string {
	if ref == nil {
		return ""
	}
	return ref.Namespace + "/" + ref.Name + "/" + string(ref.UID)
}

func MachineRefIndexValueForMachine(machine *infrastructurev1alpha1.TartMachine) string {
	return machine.Namespace + "/" + machine.Name + "/" + string(machine.UID)
}
