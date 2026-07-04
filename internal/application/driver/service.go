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

	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

const (
	defaultCallTimeout = 30 * time.Second
	defaultAttempts    = 3
)

type Invocation struct {
	OperationType string
	Phase         string
	Rollback      bool
}

type Sleeper func(context.Context, time.Duration) error
type Jitter func(time.Duration) time.Duration

type Service struct {
	registry *Registry
	timeout  time.Duration
	sleep    Sleeper
	jitter   Jitter
	calls    metric.Int64Counter
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
