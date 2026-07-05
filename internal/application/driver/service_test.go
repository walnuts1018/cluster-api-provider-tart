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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	fakedriver "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/fake"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

func TestServiceRetriesTemporaryErrorAtDefinedIntervals(t *testing.T) {
	t.Parallel()

	temporary := driverdomain.NewError(driverdomain.ErrorTemporary, errors.New("unavailable"))
	implementation := fakedriver.NewPowerOn(temporary, temporary, temporary)
	registry := NewRegistry()
	if err := registry.RegisterPowerOn("fake", implementation); err != nil {
		t.Fatalf("RegisterPowerOn() error = %v", err)
	}

	var delays []time.Duration
	service, err := NewServiceForTest(
		registry,
		30*time.Second,
		func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
		func(delay time.Duration) time.Duration { return delay },
	)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}

	err = service.PowerOn(t.Context(), "fake", testTarget(t), testOperationID(t), Invocation{})
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorTemporary) {
		t.Fatalf("PowerOn() error = %v, want Temporary", err)
	}
	if got := len(implementation.PowerOnCalls()); got != 3 {
		t.Fatalf("PowerOn calls = %d, want 3", got)
	}
	if len(delays) != 2 || delays[0] != time.Second || delays[1] != 2*time.Second {
		t.Fatalf("retry delays = %v, want [1s 2s]", delays)
	}
}

func TestServiceDoesNotRetryAuthenticationFailure(t *testing.T) {
	t.Parallel()

	authenticationFailed := driverdomain.NewError(
		driverdomain.ErrorAuthenticationFailed,
		errors.New("bad credentials"),
	)
	implementation := fakedriver.NewPowerOn(authenticationFailed)
	registry := NewRegistry()
	if err := registry.RegisterPowerOn("fake", implementation); err != nil {
		t.Fatalf("RegisterPowerOn() error = %v", err)
	}
	service, err := NewServiceForTest(registry, time.Second, sleep, func(delay time.Duration) time.Duration {
		return delay
	})
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}

	err = service.PowerOn(t.Context(), "fake", testTarget(t), testOperationID(t), Invocation{})
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorAuthenticationFailed) {
		t.Fatalf("PowerOn() error = %v, want AuthenticationFailed", err)
	}
	if got := len(implementation.PowerOnCalls()); got != 1 {
		t.Fatalf("PowerOn calls = %d, want 1", got)
	}
}

func TestServiceDeadlineDoesNotLeaveDriverWorkRunning(t *testing.T) {
	t.Parallel()

	implementation := &deadlineDriver{}
	registry := NewRegistry()
	if err := registry.RegisterPowerOn("deadline", implementation); err != nil {
		t.Fatalf("RegisterPowerOn() error = %v", err)
	}
	service, err := NewServiceForTest(registry, 10*time.Millisecond, sleep, func(delay time.Duration) time.Duration {
		return delay
	})
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}

	err = service.PowerOn(t.Context(), "deadline", testTarget(t), testOperationID(t), Invocation{})
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorDeadlineExceeded) {
		t.Fatalf("PowerOn() error = %v, want DeadlineExceeded", err)
	}
	if got := implementation.running.Load(); got != 0 {
		t.Fatalf("running driver calls = %d, want 0", got)
	}
}

func TestServiceDoesNotCallUnsupportedDriver(t *testing.T) {
	t.Parallel()

	service, err := NewServiceForTest(
		NewRegistry(),
		time.Second,
		sleep,
		func(delay time.Duration) time.Duration { return delay },
	)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}

	err = service.PowerOn(t.Context(), "missing", testTarget(t), testOperationID(t), Invocation{})
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
		t.Fatalf("PowerOn() error = %v, want Unsupported", err)
	}
}

func TestProductionJitterStaysWithinTwentyPercent(t *testing.T) {
	t.Parallel()

	const base = 10 * time.Second
	for range 100 {
		got := productionJitter(base)
		if got < 8*time.Second || got > 12*time.Second {
			t.Fatalf("productionJitter(%s) = %s, want within ±20%%", base, got)
		}
	}
}

func TestServiceMetricUsesOnlyAllowedLabels(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	originalMeter := telemetry.Meter
	telemetry.Meter = provider.Meter("driver-service-test")
	t.Cleanup(func() {
		telemetry.Meter = originalMeter
		if err := provider.Shutdown(t.Context()); err != nil {
			t.Errorf("shutdown MeterProvider: %v", err)
		}
	})

	registry := NewRegistry()
	if err := registry.RegisterPowerOn("fake", fakedriver.NewPowerOn()); err != nil {
		t.Fatalf("RegisterPowerOn() error = %v", err)
	}
	service, err := NewServiceForTest(
		registry,
		time.Second,
		sleep,
		func(delay time.Duration) time.Duration { return delay },
	)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	if err := service.PowerOn(
		t.Context(),
		"fake",
		testTarget(t),
		testOperationID(t),
		Invocation{
			OperationType: "Provision",
			Phase:         "PreparingBoot",
			Rollback:      false,
		},
	); err != nil {
		t.Fatalf("PowerOn() error = %v", err)
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(metrics.ScopeMetrics) != 1 || len(metrics.ScopeMetrics[0].Metrics) != 1 {
		t.Fatalf("collected metrics = %#v, want one metric", metrics.ScopeMetrics)
	}
	sum, ok := metrics.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("metric data = %#v, want one int64 sum data point", metrics.ScopeMetrics[0].Metrics[0].Data)
	}
	got := make(map[string]struct{})
	for _, label := range sum.DataPoints[0].Attributes.ToSlice() {
		got[string(label.Key)] = struct{}{}
	}
	want := []string{"operation_type", "phase", "driver", "result", "rollback"}
	if len(got) != len(want) {
		t.Fatalf("metric labels = %v, want exactly %v", got, want)
	}
	for _, label := range want {
		if _, exists := got[label]; !exists {
			t.Fatalf("metric labels = %v, missing %q", got, label)
		}
	}
}

type deadlineDriver struct {
	running atomic.Int64
}

func (driver *deadlineDriver) PowerOn(
	ctx context.Context,
	_ driverdomain.HostTarget,
	_ operationdomain.ID,
) error {
	driver.running.Add(1)
	defer driver.running.Add(-1)
	<-ctx.Done()
	return ctx.Err()
}

var testHelpersMu sync.Mutex

func testTarget(t *testing.T) driverdomain.HostTarget {
	t.Helper()
	address, err := driverdomain.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	return driverdomain.NewHostTarget(address)
}

func testOperationID(t *testing.T) operationdomain.ID {
	t.Helper()
	// ParseID itself is immutable; the lock only silences overly conservative
	// race instrumentation in external UUID implementations.
	testHelpersMu.Lock()
	defer testHelpersMu.Unlock()
	id, err := operationdomain.ParseID("b574b4f7-bada-43f2-a4bb-770a0c8bdce7")
	if err != nil {
		t.Fatalf("ParseID() error = %v", err)
	}
	return id
}
