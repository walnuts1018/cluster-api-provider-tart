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

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
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
			return "", driverdomain.NewError(
				driverdomain.ErrorUnsupported,
				errors.New("VirtualMedia boot requires Agent Artifact delivery integration"),
			)
		}
		if err := service.setNextBoot(ctx, name, target, *preferred, operationID, invocation); err != nil {
			return "", err
		}
		return *preferred, nil
	}

	candidates := []driverdomain.BootTarget{
		driverdomain.BootTargetHTTP,
		driverdomain.BootTargetPXE,
	}
	for _, candidate := range candidates {
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

	callCtx, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()

	for attempt := range defaultAttempts {
		err = implementation.SetNextBoot(callCtx, target, bootTarget, operationID)
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
		ctrllog.FromContext(ctx).Error(err, "Retrying temporary boot override failure",
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
	return fmt.Errorf("unreachable SetNextBoot retry state")
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
