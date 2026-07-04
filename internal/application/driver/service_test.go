package driver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fakedriver "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/fake"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
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

	err = service.PowerOn(context.Background(), "fake", testTarget(t), testOperationID(t), Invocation{})
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

	err = service.PowerOn(context.Background(), "fake", testTarget(t), testOperationID(t), Invocation{})
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

	err = service.PowerOn(context.Background(), "deadline", testTarget(t), testOperationID(t), Invocation{})
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

	err = service.PowerOn(context.Background(), "missing", testTarget(t), testOperationID(t), Invocation{})
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
		t.Fatalf("PowerOn() error = %v, want Unsupported", err)
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
