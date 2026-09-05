package driver

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/utils/telemetry"
)

const (
	defaultCallTimeout = 30 * time.Second
	defaultAttempts    = 3
)

type Invocation = driverdomain.Invocation

type Sleeper func(context.Context, time.Duration) error
type Jitter func(time.Duration) time.Duration

type Service struct {
	registry      *Registry
	agentArtifact AgentArtifactProvider
	timeout       time.Duration
	sleep         Sleeper
	jitter        Jitter
	calls         metric.Int64Counter
}

func NewService(registry *Registry) (*Service, error) {
	calls, err := telemetry.Meter.Int64Counter("tart.driver.calls")
	if err != nil {
		return nil, fmt.Errorf("create driver call counter: %w", err)
	}
	return newService(registry, defaultCallTimeout, sleep, productionJitter, calls), nil
}

func NewServiceForTest(
	registry *Registry,
	timeout time.Duration,
	sleeper Sleeper,
	jitter Jitter,
) (*Service, error) {
	calls, err := telemetry.Meter.Int64Counter("tart.driver.calls.test")
	if err != nil {
		return nil, fmt.Errorf("create driver test call counter: %w", err)
	}
	return newService(registry, timeout, sleeper, jitter, calls), nil
}

func newService(
	registry *Registry,
	timeout time.Duration,
	sleeper Sleeper,
	jitter Jitter,
	calls metric.Int64Counter,
) *Service {
	return &Service{
		registry: registry,
		timeout:  timeout,
		sleep:    sleeper,
		jitter:   jitter,
		calls:    calls,
	}
}

func (service *Service) SetAgentArtifactProvider(provider AgentArtifactProvider) {
	service.agentArtifact = provider
}

func (service *Service) PowerOn(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
	invocation Invocation,
) error {
	implementation, err := service.registry.PowerOn(name)
	if err != nil {
		service.record(ctx, name, invocation, "unsupported")
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()

	for attempt := range defaultAttempts {
		err = implementation.PowerOn(callCtx, target, operationID)
		if err == nil {
			service.record(ctx, name, invocation, "success")
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			service.record(ctx, name, invocation, "deadline_exceeded")
			return driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
		}
		if !driverdomain.IsErrorKind(err, driverdomain.ErrorTemporary) {
			service.record(ctx, name, invocation, errorResult(err))
			return err
		}
		if attempt == defaultAttempts-1 {
			service.record(ctx, name, invocation, "temporary")
			return err
		}
		delay := service.jitter(time.Duration(attempt+1) * time.Second)
		ctrllog.FromContext(ctx).Error(err, "Retrying temporary driver failure",
			"driver", name,
			"attempt", attempt+2,
			"maxAttempts", defaultAttempts,
			"retryAfter", delay,
		)
		if err := service.sleep(callCtx, delay); err != nil {
			service.record(ctx, name, invocation, errorResult(err))
			if errors.Is(err, context.DeadlineExceeded) {
				return driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
			}
			return err
		}
	}
	return fmt.Errorf("unreachable PowerOn retry state")
}

func (service *Service) DiscoverCapabilities(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	invocation Invocation,
) (capabilitydomain.Set, error) {
	if discoverer, ok := service.registry.CapabilityDiscoverer(name); ok {
		callCtx, cancel := context.WithTimeout(ctx, service.timeout)
		defer cancel()

		for attempt := range defaultAttempts {
			capabilities, err := discoverer.DiscoverCapabilities(callCtx, name, target, invocation)
			if err == nil {
				service.record(ctx, name, invocation, "success")
				return capabilities, nil
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
				service.record(ctx, name, invocation, "deadline_exceeded")
				return capabilitydomain.Set{}, driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
			}
			if !driverdomain.IsErrorKind(err, driverdomain.ErrorTemporary) {
				service.record(ctx, name, invocation, errorResult(err))
				return capabilitydomain.Set{}, err
			}
			if attempt == defaultAttempts-1 {
				service.record(ctx, name, invocation, "temporary")
				return capabilitydomain.Set{}, err
			}
			delay := service.jitter(time.Duration(attempt+1) * time.Second)
			ctrllog.FromContext(ctx).Error(err, "Retrying temporary driver discovery failure",
				"driver", name,
				"attempt", attempt+2,
				"maxAttempts", defaultAttempts,
				"retryAfter", delay,
			)
			if err := service.sleep(callCtx, delay); err != nil {
				service.record(ctx, name, invocation, errorResult(err))
				if errors.Is(err, context.DeadlineExceeded) {
					return capabilitydomain.Set{}, driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
				}
				return capabilitydomain.Set{}, err
			}
		}
		return capabilitydomain.Set{}, fmt.Errorf("unreachable DiscoverCapabilities retry state")
	}

	capabilities := service.registry.Capabilities(name)
	service.record(ctx, name, invocation, "success")
	return capabilities, nil
}

func (service *Service) ObservePowerState(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	invocation Invocation,
) (driverdomain.PowerState, error) {
	implementation, err := service.registry.PowerStateObserver(name)
	if err != nil {
		service.record(ctx, name, invocation, "unsupported")
		return driverdomain.PowerStateUnknown, err
	}

	callCtx, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()

	for attempt := range defaultAttempts {
		state, err := implementation.ObservePowerState(callCtx, target)
		if err == nil {
			service.record(ctx, name, invocation, "success")
			return state, nil
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			service.record(ctx, name, invocation, "deadline_exceeded")
			return driverdomain.PowerStateUnknown, driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
		}
		if !driverdomain.IsErrorKind(err, driverdomain.ErrorTemporary) {
			service.record(ctx, name, invocation, errorResult(err))
			return driverdomain.PowerStateUnknown, err
		}
		if attempt == defaultAttempts-1 {
			service.record(ctx, name, invocation, "temporary")
			return driverdomain.PowerStateUnknown, err
		}
		delay := service.jitter(time.Duration(attempt+1) * time.Second)
		ctrllog.FromContext(ctx).Error(err, "Retrying temporary power state observation failure",
			"driver", name,
			"attempt", attempt+2,
			"maxAttempts", defaultAttempts,
			"retryAfter", delay,
		)
		if err := service.sleep(callCtx, delay); err != nil {
			service.record(ctx, name, invocation, errorResult(err))
			if errors.Is(err, context.DeadlineExceeded) {
				return driverdomain.PowerStateUnknown, driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
			}
			return driverdomain.PowerStateUnknown, err
		}
	}
	return driverdomain.PowerStateUnknown, fmt.Errorf("unreachable ObservePowerState retry state")
}

func (service *Service) ObserveBootState(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	invocation Invocation,
) (driverdomain.BootState, error) {
	implementation, err := service.registry.BootStateObserver(name)
	if err != nil {
		service.record(ctx, name, invocation, "unsupported")
		return driverdomain.BootState{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()

	for attempt := range defaultAttempts {
		state, err := implementation.ObserveBootState(callCtx, target)
		if err == nil {
			service.record(ctx, name, invocation, "success")
			return state, nil
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			service.record(ctx, name, invocation, "deadline_exceeded")
			return driverdomain.BootState{}, driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
		}
		if !driverdomain.IsErrorKind(err, driverdomain.ErrorTemporary) {
			service.record(ctx, name, invocation, errorResult(err))
			return driverdomain.BootState{}, err
		}
		if attempt == defaultAttempts-1 {
			service.record(ctx, name, invocation, "temporary")
			return driverdomain.BootState{}, err
		}
		delay := service.jitter(time.Duration(attempt+1) * time.Second)
		ctrllog.FromContext(ctx).Error(err, "Retrying temporary boot state observation failure",
			"driver", name,
			"attempt", attempt+2,
			"maxAttempts", defaultAttempts,
			"retryAfter", delay,
		)
		if err := service.sleep(callCtx, delay); err != nil {
			service.record(ctx, name, invocation, errorResult(err))
			if errors.Is(err, context.DeadlineExceeded) {
				return driverdomain.BootState{}, driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
			}
			return driverdomain.BootState{}, err
		}
	}
	return driverdomain.BootState{}, fmt.Errorf("unreachable ObserveBootState retry state")
}

func (service *Service) PrepareBoot(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
	preferred *driverdomain.BootTarget,
	invocation Invocation,
) (driverdomain.BootTarget, error) {
	if preferred != nil {
		if *preferred == driverdomain.BootTargetVirtualMedia {
			if err := service.prepareVirtualMedia(ctx, name, target, operationID, invocation); err != nil {
				return "", err
			}
			if err := service.setNextBoot(ctx, name, target, *preferred, operationID, invocation); err != nil {
				return "", err
			}
			return *preferred, nil
		}
		if err := service.setNextBoot(ctx, name, target, *preferred, operationID, invocation); err != nil {
			return "", err
		}
		return *preferred, nil
	}

	candidates := []driverdomain.BootTarget{
		driverdomain.BootTargetHTTP,
		driverdomain.BootTargetVirtualMedia,
		driverdomain.BootTargetPXE,
	}
	for _, candidate := range candidates {
		if candidate == driverdomain.BootTargetVirtualMedia {
			if err := service.prepareVirtualMedia(ctx, name, target, operationID, invocation); err != nil {
				if driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
					continue
				}
				return "", err
			}
		}
		if err := service.setNextBoot(ctx, name, target, candidate, operationID, invocation); err != nil {
			if driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
				continue
			}
			return "", err
		}
		return candidate, nil
	}
	return "", driverdomain.NewError(
		driverdomain.ErrorUnsupported,
		errors.New("no supported Redfish network boot transport is available"),
	)
}

func (service *Service) prepareVirtualMedia(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
	invocation Invocation,
) error {
	if service.agentArtifact == nil {
		service.record(ctx, name, invocation, "unsupported")
		return driverdomain.NewError(
			driverdomain.ErrorUnsupported,
			errors.New("VirtualMedia boot requires Agent Artifact delivery integration"),
		)
	}
	implementation, err := service.registry.VirtualMedia(name)
	if err != nil {
		service.record(ctx, name, invocation, "unsupported")
		return err
	}
	artifact, err := service.agentArtifact.VirtualMediaArtifact(ctx, operationID)
	if err != nil {
		service.record(ctx, name, invocation, errorResult(err))
		return err
	}

	return service.retryDriverCall(
		ctx,
		name,
		invocation,
		"Retrying temporary virtual media failure",
		func(callCtx context.Context) error {
			return implementation.Mount(callCtx, target, artifact, operationID)
		},
	)
}

func (service *Service) setNextBoot(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	bootTarget driverdomain.BootTarget,
	operationID operationdomain.ID,
	invocation Invocation,
) error {
	implementation, err := service.registry.BootOverride(name)
	if err != nil {
		service.record(ctx, name, invocation, "unsupported")
		return err
	}

	return service.retryDriverCall(
		ctx,
		name,
		invocation,
		"Retrying temporary boot override failure",
		func(callCtx context.Context) error {
			return implementation.SetNextBoot(callCtx, target, bootTarget, operationID)
		},
	)
}

func (service *Service) retryDriverCall(
	ctx context.Context,
	name driverdomain.Name,
	invocation Invocation,
	retryMessage string,
	call func(context.Context) error,
) error {
	callCtx, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()

	for attempt := range defaultAttempts {
		err := call(callCtx)
		if err == nil {
			service.record(ctx, name, invocation, "success")
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			service.record(ctx, name, invocation, "deadline_exceeded")
			return driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
		}
		if !driverdomain.IsErrorKind(err, driverdomain.ErrorTemporary) {
			service.record(ctx, name, invocation, errorResult(err))
			return err
		}
		if attempt == defaultAttempts-1 {
			service.record(ctx, name, invocation, "temporary")
			return err
		}
		delay := service.jitter(time.Duration(attempt+1) * time.Second)
		ctrllog.FromContext(ctx).Error(err, retryMessage,
			"driver", name,
			"attempt", attempt+2,
			"maxAttempts", defaultAttempts,
			"retryAfter", delay,
		)
		if err := service.sleep(callCtx, delay); err != nil {
			service.record(ctx, name, invocation, errorResult(err))
			if errors.Is(err, context.DeadlineExceeded) {
				return driverdomain.NewError(driverdomain.ErrorDeadlineExceeded, err)
			}
			return err
		}
	}
	return errors.New("unreachable driver retry state")
}

func (service *Service) record(
	ctx context.Context,
	name driverdomain.Name,
	invocation Invocation,
	result string,
) {
	service.calls.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation_type", invocation.OperationType),
		attribute.String("phase", invocation.Phase),
		attribute.String("driver", string(name)),
		attribute.String("result", result),
		attribute.Bool("rollback", invocation.Rollback),
	))
}

func errorResult(err error) string {
	switch {
	case driverdomain.IsErrorKind(err, driverdomain.ErrorAuthenticationFailed):
		return "authentication_failed"
	case driverdomain.IsErrorKind(err, driverdomain.ErrorTemporary):
		return "temporary"
	case driverdomain.IsErrorKind(err, driverdomain.ErrorDeadlineExceeded):
		return "deadline_exceeded"
	case driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported):
		return "unsupported"
	}
	return "error"
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func productionJitter(delay time.Duration) time.Duration {
	factor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(delay) * factor)
}
