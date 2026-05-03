package telemetry

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	ServiceName    string
	ServiceVersion string
}

func NewProvider(ctx context.Context) (*Provider, error) {
	p := &Provider{
		ServiceName:    defaultOTELServiceName,
		ServiceVersion: "latest",
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
		return nil, fmt.Errorf("failed to create meter provider: %w", err)
	}
	p.MeterProvider = mp

	return p, nil
}

func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error

	if p.TracerProvider != nil {
		if err := p.TracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown tracer provider: %w", err))
		}
	}

	if p.MeterProvider != nil {
		if err := p.MeterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown meter provider: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

func SetupSignalHandler(ctx context.Context, shutdownFn func(context.Context) error) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "Received signal %s, shutting down...\n", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := shutdownFn(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to shutdown: %v\n", err)
		}
		os.Exit(0)
	}()
}
