package fake

import (
	"context"
	"errors"
	"testing"
)

func TestBackendError列と呼び出し回数(t *testing.T) {
	powerOnFirst := errors.New("power on first error")
	powerOnSecond := errors.New("power on second error")
	powerOffFirst := errors.New("power off first error")
	backend := New(
		WithPowerState("On"),
		WithPowerOnErrors(powerOnFirst, powerOnSecond),
		WithPowerOffErrors(powerOffFirst),
	)

	for _, want := range []error{powerOnFirst, powerOnSecond, nil} {
		if err := backend.PowerOn(t.Context()); !errors.Is(err, want) {
			t.Errorf("PowerOn() error = %v, want %v", err, want)
		}
	}
	for _, want := range []error{powerOffFirst, nil} {
		if err := backend.PowerOff(t.Context()); !errors.Is(err, want) {
			t.Errorf("PowerOff() error = %v, want %v", err, want)
		}
	}
	state, err := backend.PowerState(t.Context())
	if err != nil || state != "On" {
		t.Fatalf("PowerState() = %q, %v, want %q, nil", state, err, "On")
	}

	powerOnCalls, powerOffCalls, powerStateCalls := backend.Calls()
	if powerOnCalls != 3 || powerOffCalls != 2 || powerStateCalls != 1 {
		t.Fatalf("Calls() = %d, %d, %d, want 3, 2, 1", powerOnCalls, powerOffCalls, powerStateCalls)
	}
}

func TestBackendPowerStateError(t *testing.T) {
	wantErr := errors.New("power state unavailable")
	backend := New(WithPowerStateError(wantErr))

	state, err := backend.PowerState(t.Context())
	if !errors.Is(err, wantErr) {
		t.Fatalf("PowerState() error = %v, want %v", err, wantErr)
	}
	if state != "" {
		t.Fatalf("PowerState() state = %q, want empty", state)
	}
}

func TestBackendContextエラーは呼び出しを記録しない(t *testing.T) {
	backend := New()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := backend.PowerOn(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("PowerOn() error = %v, want context.Canceled", err)
	}
	if err := backend.PowerOff(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("PowerOff() error = %v, want context.Canceled", err)
	}
	if _, err := backend.PowerState(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("PowerState() error = %v, want context.Canceled", err)
	}

	if powerOnCalls, powerOffCalls, powerStateCalls := backend.Calls(); powerOnCalls != 0 || powerOffCalls != 0 || powerStateCalls != 0 {
		t.Fatalf("キャンセル済みContext後のCalls() = %d, %d, %d, want 0, 0, 0", powerOnCalls, powerOffCalls, powerStateCalls)
	}
}
