// Package fakeはテスト用の電源backend実装を提供する。old/infrastructure/service/driver/fake配下の旧実装を統合し、adapter/powerのPowerOn/PowerOff/PowerStateObserverを満たす。
package fake

import (
	"context"
	"sync"
)

// Backendは呼び出しを記録し、設定されたerror列を順番に返すテスト用backendである。
type Backend struct {
	mu              sync.Mutex
	powerState      string
	powerStateErr   error
	powerOnErrors   []error
	powerOffErrors  []error
	powerOnCalls    int
	powerOffCalls   int
	powerStateCalls int
}

// OptionはBackendの初期状態を設定する。
type Option func(*Backend)

// WithPowerStateは観測される電源状態を設定する。
func WithPowerState(state string) Option {
	return func(b *Backend) {
		b.powerState = state
	}
}

// WithPowerStateErrorはPowerState呼び出し時のerrorを設定する。
func WithPowerStateError(err error) Option {
	return func(b *Backend) {
		b.powerStateErr = err
	}
}

// WithPowerOnErrorsはPowerOn呼び出し時に順番に返すerror列を設定する。
func WithPowerOnErrors(errors ...error) Option {
	return func(b *Backend) {
		b.powerOnErrors = append([]error(nil), errors...)
	}
}

// WithPowerOffErrorsはPowerOff呼び出し時に順番に返すerror列を設定する。
func WithPowerOffErrors(errors ...error) Option {
	return func(b *Backend) {
		b.powerOffErrors = append([]error(nil), errors...)
	}
}

// NewはOptionを適用したBackendを生成する。
func New(options ...Option) *Backend {
	backend := &Backend{powerState: "Unknown"}
	for _, option := range options {
		option(backend)
	}
	return backend
}

// PowerOnは呼び出しを記録し、設定されたerrorを返す。
func (b *Backend) PowerOn(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	index := b.powerOnCalls
	b.powerOnCalls++
	if index < len(b.powerOnErrors) {
		return b.powerOnErrors[index]
	}
	return nil
}

// PowerOffは呼び出しを記録し、設定されたerrorを返す。
func (b *Backend) PowerOff(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	index := b.powerOffCalls
	b.powerOffCalls++
	if index < len(b.powerOffErrors) {
		return b.powerOffErrors[index]
	}
	return nil
}

// PowerStateは設定された電源状態を返す。
func (b *Backend) PowerState(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.powerStateCalls++
	if b.powerStateErr != nil {
		return "", b.powerStateErr
	}
	return b.powerState, nil
}

// Callsは呼び出し回数を返す。
func (b *Backend) Calls() (powerOn, powerOff, powerState int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.powerOnCalls, b.powerOffCalls, b.powerStateCalls
}
