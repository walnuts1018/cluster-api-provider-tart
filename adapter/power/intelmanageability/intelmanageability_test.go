package intelmanageability

import (
	"context"
	"errors"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/power"
)

// fakeManageabilityClientはBackendのpolicyだけを検証するための最小限のfakeである。WS-Man protocolや
// go-wsman-messagesを一切介さない。
type fakeManageabilityClient struct {
	state        cimPowerState
	stateErr     error
	invoked      bool
	invokedState cimPowerState
	requestErr   error
}

func (f *fakeManageabilityClient) currentPowerState(ctx context.Context) (cimPowerState, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.state, f.stateErr
}

func (f *fakeManageabilityClient) requestPowerStateChange(ctx context.Context, state cimPowerState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.invoked = true
	f.invokedState = state
	return f.requestErr
}

func TestBackendPowerOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		state         cimPowerState
		wantErr       bool
		wantInvoked   bool
		wantPowerCode cimPowerState
	}{
		{name: "Onなら遷移要求しない", state: dmtfPowerOn, wantInvoked: false},
		{name: "Offから電源投入を要求する", state: dmtfPowerOffSoft, wantInvoked: true, wantPowerCode: dmtfPowerOn},
		{name: "Unknownからは拒否する", state: cimPowerState(0), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeManageabilityClient{state: test.state}
			backend := &Backend{client: fake}
			err := backend.PowerOn(t.Context())
			if (err != nil) != test.wantErr {
				t.Fatalf("PowerOn() error = %v, wantErr = %t", err, test.wantErr)
			}
			if test.wantErr {
				if !errors.Is(err, ErrUnexpectedPowerState) {
					t.Fatalf("PowerOn() error = %v, want ErrUnexpectedPowerState", err)
				}
				return
			}
			if fake.invoked != test.wantInvoked {
				t.Fatalf("invoked = %t, want %t", fake.invoked, test.wantInvoked)
			}
			if test.wantInvoked && fake.invokedState != test.wantPowerCode {
				t.Fatalf("invokedState = %d, want %d", fake.invokedState, test.wantPowerCode)
			}
		})
	}
}

func TestBackendPowerOff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		state         cimPowerState
		wantErr       bool
		wantInvoked   bool
		wantPowerCode cimPowerState
	}{
		{name: "Offなら遷移要求しない", state: dmtfPowerOffSoft, wantInvoked: false},
		{name: "Onからsoft power offを要求する", state: dmtfPowerOn, wantInvoked: true, wantPowerCode: dmtfPowerOffSoft},
		{name: "Unknownからは拒否する", state: cimPowerState(0), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeManageabilityClient{state: test.state}
			backend := &Backend{client: fake}
			err := backend.PowerOff(t.Context())
			if (err != nil) != test.wantErr {
				t.Fatalf("PowerOff() error = %v, wantErr = %t", err, test.wantErr)
			}
			if test.wantErr {
				if !errors.Is(err, ErrUnexpectedPowerState) {
					t.Fatalf("PowerOff() error = %v, want ErrUnexpectedPowerState", err)
				}
				return
			}
			if fake.invoked != test.wantInvoked {
				t.Fatalf("invoked = %t, want %t", fake.invoked, test.wantInvoked)
			}
			if test.wantInvoked && fake.invokedState != test.wantPowerCode {
				t.Fatalf("invokedState = %d, want %d", fake.invokedState, test.wantPowerCode)
			}
		})
	}
}

func TestBackendPowerCycle(t *testing.T) {
	t.Parallel()

	onFake := &fakeManageabilityClient{state: dmtfPowerOn}
	onBackend := &Backend{client: onFake}
	if err := onBackend.PowerCycle(t.Context()); err != nil {
		t.Fatalf("PowerCycle() error = %v", err)
	}
	if !onFake.invoked || onFake.invokedState != dmtfPowerMasterBusReset {
		t.Fatalf("invoked = %t, invokedState = %d, want true, %d", onFake.invoked, onFake.invokedState, dmtfPowerMasterBusReset)
	}

	offFake := &fakeManageabilityClient{state: dmtfPowerOffSoft}
	offBackend := &Backend{client: offFake}
	if err := offBackend.PowerCycle(t.Context()); !errors.Is(err, ErrUnexpectedPowerState) {
		t.Fatalf("PowerCycle() error = %v, want ErrUnexpectedPowerState", err)
	}
}

func TestBackendPowerState取得不能はUnknown(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	fake := &fakeManageabilityClient{stateErr: wantErr}
	backend := &Backend{client: fake}
	state, err := backend.PowerState(t.Context())
	if !errors.Is(err, wantErr) {
		t.Fatalf("PowerState() error = %v, want %v", err, wantErr)
	}
	if state != power.PowerStateUnknown {
		t.Fatalf("PowerState() = %q, want Unknown", state)
	}
}

func TestMapCIMPowerState(t *testing.T) {
	t.Parallel()

	tests := map[cimPowerState]power.PowerState{
		dmtfPowerOn:              power.PowerStateOn,
		dmtfPowerOffHard:         power.PowerStateOff,
		dmtfPowerOffSoft:         power.PowerStateOff,
		dmtfPowerOffSoftGraceful: power.PowerStateOff,
		dmtfPowerOffHardGraceful: power.PowerStateOff,
		cimPowerState(0):         power.PowerStateUnknown,
		cimPowerState(3):         power.PowerStateUnknown,
	}
	for input, want := range tests {
		if got := mapCIMPowerState(input); got != want {
			t.Fatalf("mapCIMPowerState(%d) = %q, want %q", input, got, want)
		}
	}
}
