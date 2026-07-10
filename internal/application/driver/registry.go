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
	"fmt"
	"sync"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
)

type Registry struct {
	mu                  sync.RWMutex
	powerOn             map[driverdomain.Name]PowerOnDriver
	powerOff            map[driverdomain.Name]PowerOffDriver
	powerStateObservers map[driverdomain.Name]PowerStateObserver
	bootOverride        map[driverdomain.Name]BootOverrideDriver
	virtualMedia        map[driverdomain.Name]VirtualMediaDriver
	discoverers         map[driverdomain.Name]CapabilityDiscoverer
}

func NewRegistry() *Registry {
	return &Registry{
		powerOn:             make(map[driverdomain.Name]PowerOnDriver),
		powerOff:            make(map[driverdomain.Name]PowerOffDriver),
		powerStateObservers: make(map[driverdomain.Name]PowerStateObserver),
		bootOverride:        make(map[driverdomain.Name]BootOverrideDriver),
		virtualMedia:        make(map[driverdomain.Name]VirtualMediaDriver),
		discoverers:         make(map[driverdomain.Name]CapabilityDiscoverer),
	}
}

func (registry *Registry) RegisterPowerOn(name driverdomain.Name, implementation PowerOnDriver) error {
	if implementation == nil {
		return fmt.Errorf("register PowerOn driver %q: implementation must not be nil", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.powerOn[name]; exists {
		return fmt.Errorf("register PowerOn driver %q: already registered", name)
	}
	registry.powerOn[name] = implementation
	return nil
}

func (registry *Registry) RegisterPowerOff(name driverdomain.Name, implementation PowerOffDriver) error {
	if implementation == nil {
		return fmt.Errorf("register PowerOff driver %q: implementation must not be nil", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.powerOff[name]; exists {
		return fmt.Errorf("register PowerOff driver %q: already registered", name)
	}
	registry.powerOff[name] = implementation
	return nil
}

func (registry *Registry) RegisterPowerStateObserver(name driverdomain.Name, implementation PowerStateObserver) error {
	if implementation == nil {
		return fmt.Errorf("register ObservePowerState driver %q: implementation must not be nil", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.powerStateObservers[name]; exists {
		return fmt.Errorf("register ObservePowerState driver %q: already registered", name)
	}
	registry.powerStateObservers[name] = implementation
	return nil
}

func (registry *Registry) RegisterBootOverride(name driverdomain.Name, implementation BootOverrideDriver) error {
	if implementation == nil {
		return fmt.Errorf("register SetNextBoot driver %q: implementation must not be nil", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.bootOverride[name]; exists {
		return fmt.Errorf("register SetNextBoot driver %q: already registered", name)
	}
	registry.bootOverride[name] = implementation
	return nil
}

func (registry *Registry) RegisterVirtualMedia(name driverdomain.Name, implementation VirtualMediaDriver) error {
	if implementation == nil {
		return fmt.Errorf("register VirtualMedia driver %q: implementation must not be nil", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.virtualMedia[name]; exists {
		return fmt.Errorf("register VirtualMedia driver %q: already registered", name)
	}
	registry.virtualMedia[name] = implementation
	return nil
}

func (registry *Registry) RegisterCapabilityDiscoverer(name driverdomain.Name, implementation CapabilityDiscoverer) error {
	if implementation == nil {
		return fmt.Errorf("register capability discoverer %q: implementation must not be nil", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.discoverers[name]; exists {
		return fmt.Errorf("register capability discoverer %q: already registered", name)
	}
	registry.discoverers[name] = implementation
	return nil
}

func (registry *Registry) PowerOn(name driverdomain.Name) (PowerOnDriver, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	implementation, exists := registry.powerOn[name]
	if !exists {
		return nil, unsupported(name, capabilitydomain.PowerOn)
	}
	return implementation, nil
}

func (registry *Registry) PowerOff(name driverdomain.Name) (PowerOffDriver, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	implementation, exists := registry.powerOff[name]
	if !exists {
		return nil, unsupported(name, capabilitydomain.PowerOff)
	}
	return implementation, nil
}

func (registry *Registry) PowerStateObserver(name driverdomain.Name) (PowerStateObserver, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	implementation, exists := registry.powerStateObservers[name]
	if !exists {
		return nil, unsupported(name, capabilitydomain.ObservePowerState)
	}
	return implementation, nil
}

func (registry *Registry) BootOverride(name driverdomain.Name) (BootOverrideDriver, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	implementation, exists := registry.bootOverride[name]
	if !exists {
		return nil, unsupported(name, capabilitydomain.SetNextBoot)
	}
	return implementation, nil
}

func (registry *Registry) VirtualMedia(name driverdomain.Name) (VirtualMediaDriver, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	implementation, exists := registry.virtualMedia[name]
	if !exists {
		return nil, unsupported(name, capabilitydomain.VirtualMedia)
	}
	return implementation, nil
}

func unsupported(name driverdomain.Name, capability capabilitydomain.Capability) error {
	return driverdomain.NewError(
		driverdomain.ErrorUnsupported,
		fmt.Errorf("driver %q does not provide %s", name, capability),
	)
}

func (registry *Registry) CapabilityDiscoverer(name driverdomain.Name) (CapabilityDiscoverer, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	implementation, exists := registry.discoverers[name]
	return implementation, exists
}

func (registry *Registry) Capabilities(name driverdomain.Name) capabilitydomain.Set {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	capabilities := make([]capabilitydomain.Capability, 0, 5)
	if _, exists := registry.powerOn[name]; exists {
		capabilities = append(capabilities, capabilitydomain.PowerOn)
	}
	if _, exists := registry.powerOff[name]; exists {
		capabilities = append(capabilities, capabilitydomain.PowerOff)
	}
	if _, exists := registry.powerStateObservers[name]; exists {
		capabilities = append(capabilities, capabilitydomain.ObservePowerState)
	}
	if _, exists := registry.bootOverride[name]; exists {
		capabilities = append(capabilities, capabilitydomain.SetNextBoot)
	}
	if _, exists := registry.virtualMedia[name]; exists {
		capabilities = append(capabilities, capabilitydomain.VirtualMedia)
	}
	result, err := capabilitydomain.NewSet(capabilities...)
	if err != nil {
		panic(fmt.Sprintf("registry produced invalid capabilities: %v", err))
	}
	return result
}
