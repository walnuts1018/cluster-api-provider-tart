package provisioning

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

type WakeOnLANSender interface {
	Send(macAddress string) error
}

type HostService interface {
	MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
}

type Service interface {
	Begin(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
	Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
}

type service struct {
	client      client.Client
	hostService HostService
	wolSender   WakeOnLANSender
}

func NewService(k8sClient client.Client, hostService HostService, wolSender WakeOnLANSender) Service {
	return &service{
		client:      k8sClient,
		hostService: hostService,
		wolSender:   wolSender,
	}
}

func (s *service) Begin(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
	if err := s.wolSender.Send(hostdomain.BootMACAddress(host)); err != nil {
		return fmt.Errorf("failed to send wake-on-lan: %w", err)
	}
	return s.hostService.MarkProvisioning(ctx, host)
}

func (s *service) Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	if machine.Status.HostRef == nil {
		return nil
	}

	var currentHost infrastructurev1alpha1.TartHost
	if err := s.client.Get(ctx, types.NamespacedName{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, &currentHost); err != nil {
		return err
	}

	if currentHost.Status.State == infrastructurev1alpha1.TartHostStateProvisioning ||
		currentHost.Status.State == infrastructurev1alpha1.TartHostStateProvisioned {
		return nil
	}

	return s.Begin(ctx, &currentHost)
}
