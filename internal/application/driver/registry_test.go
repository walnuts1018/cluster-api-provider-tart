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
	"errors"
	"testing"

	fakedriver "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/fake"
	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
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

func TestDriverErrorCanBeMatchedThroughWrapping(t *testing.T) {
	t.Parallel()

	err := driverdomain.NewError(driverdomain.ErrorAuthenticationFailed, errors.New("bad credentials"))
	if !driverdomain.IsErrorKind(err, driverdomain.ErrorAuthenticationFailed) {
		t.Fatal("wrapped driver error was not classified")
	}
}
