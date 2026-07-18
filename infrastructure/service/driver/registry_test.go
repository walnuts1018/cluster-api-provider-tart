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
	"testing"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	fakedriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver/fake"
)

func TestRegistryRejectsUnsupportedPowerOnBeforeCallingDriver(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	implementation, err := registry.PowerOn("missing")
	if implementation != nil {
		t.Fatal("PowerOn() returned an implementation for an unsupported driver")
	}
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
		t.Fatalf("PowerOn() error = %v, want Unsupported", err)
	}
}

func TestRegistryReportsOnlyRegisteredCapabilities(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.RegisterPowerOn(driverdomain.WoL, fakedriver.NewPowerOn()); err != nil {
		t.Fatalf("RegisterPowerOn() error = %v", err)
	}

	capabilities := registry.Capabilities(driverdomain.WoL)
	if got := capabilities.Values(); len(got) != 1 || got[0] != capabilitydomain.PowerOn {
		t.Fatalf("Capabilities() = %v, want [PowerOn]", got)
	}
}

func TestRegistryRejectsDuplicatePowerOnRegistration(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.RegisterPowerOn(driverdomain.WoL, fakedriver.NewPowerOn()); err != nil {
		t.Fatalf("first RegisterPowerOn() error = %v", err)
	}
	if err := registry.RegisterPowerOn(driverdomain.WoL, fakedriver.NewPowerOn()); err == nil {
		t.Fatal("second RegisterPowerOn() error = nil, want duplicate error")
	}
}

func TestRegistryReturnsRegisteredCapabilityDiscoverer(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	discoverer := &fakeCapabilityDiscoverer{}
	if err := registry.RegisterCapabilityDiscoverer(driverdomain.Redfish, discoverer); err != nil {
		t.Fatalf("RegisterCapabilityDiscoverer() error = %v", err)
	}
	got, ok := registry.CapabilityDiscoverer(driverdomain.Redfish)
	if !ok || got == nil {
		t.Fatal("CapabilityDiscoverer() = nil/false, want registered implementation")
	}
}

func TestDriverErrorCanBeMatchedThroughWrapping(t *testing.T) {
	t.Parallel()

	err := driverdomain.NewError(driverdomain.ErrorAuthenticationFailed, errors.New("bad credentials"))
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorAuthenticationFailed) {
		t.Fatal("wrapped driver error was not classified")
	}
}

type fakeCapabilityDiscoverer struct{}

func (*fakeCapabilityDiscoverer) DiscoverCapabilities(
	context.Context,
	driverdomain.Name,
	driverdomain.HostTarget,
	Invocation,
) (capabilitydomain.Set, error) {
	return capabilitydomain.NewSet(capabilitydomain.PowerOn)
}
