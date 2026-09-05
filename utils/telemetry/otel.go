package telemetry

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
)

func serviceVersion() string {
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

type Provider struct {
	TracerProvider TraceProvider
	MeterProvider  MeterProvider
	ServiceName    string
	ServiceVersion string
}

func NewProvider(ctx context.Context) (*Provider, error) {
	p := &Provider{
		ServiceName:    defaultOTELServiceName,
		ServiceVersion: serviceVersion(),
	}

	tp, err := NewTracerProvider(ctx, TracerProviderConfig{
		ServiceName:    p.ServiceName,
		ServiceVersion: p.ServiceVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer provider: %w", err)
	}
	p.TracerProvider = tp

	mp, err := NewMeterProvider(ctx, MeterProviderConfig{
		ServiceName:    p.ServiceName,
		ServiceVersion: p.ServiceVersion,
	})
	if err != nil {
		if shutdownErr := tp.Shutdown(ctx); shutdownErr != nil {
			return nil, errors.Join(
				fmt.Errorf("failed to create meter provider: %w", err),
				fmt.Errorf("failed to shutdown tracer provider after meter setup failed: %w", shutdownErr),
			)
		}
		return nil, fmt.Errorf("failed to create meter provider: %w", err)
	}
	p.MeterProvider = mp

	return p, nil
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var errs []error

	if err := p.TracerProvider.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to shutdown tracer provider: %w", err))
	}

	if err := p.MeterProvider.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to shutdown meter provider: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %w", errors.Join(errs...))
	}
	return nil
}
