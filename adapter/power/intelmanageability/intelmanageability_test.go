package intelmanageability

import (
	"context"
	"encoding/xml"
	"errors"
	"testing"
)

// fakeWSManClientはBackendのpower遷移ロジックだけをHTTP/XMLなしで検証するためのtest doubleである。
type fakeWSManClient struct {
	powerState        string
	powerStateErr     error
	invokedPowerState string
	invokeErr         error
	invokeCalls       int
}

func (f *fakeWSManClient) get(context.Context, string) (*xmlNode, error) {
	return nil, errors.New("get is not used by Backend")
}

func (f *fakeWSManClient) enumerateAll(context.Context, string) ([]*xmlNode, error) {
	if f.powerStateErr != nil {
		return nil, f.powerStateErr
	}
	if f.powerState == "" {
		return nil, nil
	}
	return []*xmlNode{
		{Children: []xmlNode{{XMLName: xml.Name{Local: "PowerState"}, Content: f.powerState}}},
	}, nil
}

func (f *fakeWSManClient) invoke(_ context.Context, _, _ string, params []invokeParam) (*xmlNode, error) {
	f.invokeCalls++
	for _, param := range params {
		if param.name == "PowerState" {
			f.invokedPowerState = param.value
		}
	}
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return &xmlNode{}, nil
}

func TestBackendPowerOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		state         string
		wantErr       bool
		wantInvoked   bool
		wantPowerCode string
	}{
		{name: "Onなら遷移要求しない", state: cimPowerStateOn, wantInvoked: false},
		{name: "Offから電源投入を要求する", state: cimPowerStateOffSoft, wantInvoked: true, wantPowerCode: cimPowerStateOn},
		{name: "未知の状態からは拒否する", state: "999", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeWSManClient{powerState: test.state}
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
			if (fake.invokeCalls > 0) != test.wantInvoked {
				t.Fatalf("invokeCalls = %d, wantInvoked = %t", fake.invokeCalls, test.wantInvoked)
			}
			if test.wantInvoked && fake.invokedPowerState != test.wantPowerCode {
				t.Fatalf("invokedPowerState = %q, want %q", fake.invokedPowerState, test.wantPowerCode)
			}
		})
	}
}

func TestBackendPowerOff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		state         string
		wantErr       bool
		wantInvoked   bool
		wantPowerCode string
	}{
		{name: "Offなら遷移要求しない", state: cimPowerStateOffSoft, wantInvoked: false},
		{name: "Onからsoft power offを要求する", state: cimPowerStateOn, wantInvoked: true, wantPowerCode: cimPowerStateOffSoft},
		{name: "未知の状態からは拒否する", state: "999", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeWSManClient{powerState: test.state}
			backend := &Backend{client: fake}

			err := backend.PowerOff(t.Context())
			if (err != nil) != test.wantErr {
				t.Fatalf("PowerOff() error = %v, wantErr = %t", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if (fake.invokeCalls > 0) != test.wantInvoked {
				t.Fatalf("invokeCalls = %d, wantInvoked = %t", fake.invokeCalls, test.wantInvoked)
			}
			if test.wantInvoked && fake.invokedPowerState != test.wantPowerCode {
				t.Fatalf("invokedPowerState = %q, want %q", fake.invokedPowerState, test.wantPowerCode)
			}
		})
	}
}

func TestBackendPowerCycleはOnからのみ許可する(t *testing.T) {
	t.Parallel()

	onFake := &fakeWSManClient{powerState: cimPowerStateOn}
	onBackend := &Backend{client: onFake}
	if err := onBackend.PowerCycle(t.Context()); err != nil {
		t.Fatalf("PowerCycle() error = %v", err)
	}
	if onFake.invokedPowerState != cimPowerStateMasterBusReset {
		t.Fatalf("invokedPowerState = %q, want %q", onFake.invokedPowerState, cimPowerStateMasterBusReset)
	}

	offFake := &fakeWSManClient{powerState: cimPowerStateOffSoft}
	offBackend := &Backend{client: offFake}
	if err := offBackend.PowerCycle(t.Context()); !errors.Is(err, ErrUnexpectedPowerState) {
		t.Fatalf("PowerCycle() error = %v, want ErrUnexpectedPowerState", err)
	}
	if offFake.invokeCalls != 0 {
		t.Fatalf("invokeCalls = %d, want 0", offFake.invokeCalls)
	}
}

func TestBackendPowerState取得不能はUnknown(t *testing.T) {
	t.Parallel()

	fake := &fakeWSManClient{powerStateErr: ErrConnectionFailed}
	backend := &Backend{client: fake}

	state, err := backend.PowerState(t.Context())
	if !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("PowerState() error = %v, want ErrConnectionFailed", err)
	}
	if state != PowerStateUnknown {
		t.Fatalf("PowerState() = %q, want Unknown", state)
	}
}

func TestMapCIMPowerState(t *testing.T) {
	t.Parallel()

	tests := map[string]PowerState{
		cimPowerStateOn:              PowerStateOn,
		cimPowerStateOffHard:         PowerStateOff,
		cimPowerStateOffSoft:         PowerStateOff,
		cimPowerStateOffSoftGraceful: PowerStateOff,
		cimPowerStateOffHardGraceful: PowerStateOff,
		"3":                          PowerStateUnknown,
		"":                           PowerStateUnknown,
	}
	for value, want := range tests {
		if got := mapCIMPowerState(value); got != want {
			t.Errorf("mapCIMPowerState(%q) = %q, want %q", value, got, want)
		}
	}
}
