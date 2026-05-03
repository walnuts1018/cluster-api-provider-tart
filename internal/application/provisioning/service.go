package provisioning

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

type WakeOnLANSender interface {
	Send(macAddress string) error
}

type HostReader interface {
	GetAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error)
}

type HostProvisioner interface {
	MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
}

type Service interface {
	Begin(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
	Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
}

type service struct {
	hostReader      HostReader
	hostProvisioner HostProvisioner
	wolSender       WakeOnLANSender
}

func NewService(hostReader HostReader, hostProvisioner HostProvisioner, wolSender WakeOnLANSender) Service {
	return &service{
		hostReader:      hostReader,
		hostProvisioner: hostProvisioner,
		wolSender:       wolSender,
	}
}

func (s *service) Begin(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
	log := ctrllog.FromContext(ctx).WithName("provisioning")
	if err := retry.Do(
		func() error {
			return s.wolSender.Send(hostdomain.BootMACAddress(host))
		},
		retry.Context(ctx),
		retry.MaxDelay(2*time.Second),
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(n uint, err error) {
			log.Error(err, "Retrying WoL send", "attempt", n+1, "maxAttempts", 3, "mac", hostdomain.BootMACAddress(host))
		}),
	); err != nil {
		return fmt.Errorf("failed to send wake-on-lan after retries: %w", err)
	}
	return s.hostProvisioner.MarkProvisioning(ctx, host)
}

func (s *service) Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	if machine.Status.HostRef == nil {
		return nil
	}

	host, err := s.hostReader.GetAssigned(ctx, machine)
	if err != nil {
		return err
	}

	if !hostdomain.MachineRefMatches(host.Status.MachineRef, machine) {
		return nil
	}

	if host.Status.State == infrastructurev1alpha1.TartHostStateProvisioning ||
		host.Status.State == infrastructurev1alpha1.TartHostStateProvisioned {
		return nil
	}

	return s.Begin(ctx, host)
}
