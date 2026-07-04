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
}

func NewRegistry() *Registry {
	return &Registry{
		powerOn:             make(map[driverdomain.Name]PowerOnDriver),
		powerOff:            make(map[driverdomain.Name]PowerOffDriver),
		powerStateObservers: make(map[driverdomain.Name]PowerStateObserver),
		bootOverride:        make(map[driverdomain.Name]BootOverrideDriver),
		virtualMedia:        make(map[driverdomain.Name]VirtualMediaDriver),
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
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.powerOff[name] = implementation
	return nil
}

func (registry *Registry) RegisterPowerStateObserver(name driverdomain.Name, implementation PowerStateObserver) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.powerStateObservers[name] = implementation
	return nil
}

func (registry *Registry) RegisterBootOverride(name driverdomain.Name, implementation BootOverrideDriver) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.bootOverride[name] = implementation
	return nil
}

func (registry *Registry) RegisterVirtualMedia(name driverdomain.Name, implementation VirtualMediaDriver) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.virtualMedia[name] = implementation
	return nil
}

func (registry *Registry) PowerOn(name driverdomain.Name) (PowerOnDriver, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	implementation, exists := registry.powerOn[name]
	if !exists {
		return nil, driverdomain.NewError(
			driverdomain.ErrorUnsupported,
			fmt.Errorf("driver %q does not provide %s", name, capabilitydomain.PowerOn),
		)
	}
	return implementation, nil
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
