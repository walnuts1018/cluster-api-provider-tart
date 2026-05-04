package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var Meter = otel.Meter("github.com/walnuts1018/cluster-api-provider-tart")

type MeterProviderConfig struct {
	ServiceName    string
	ServiceVersion string
}

func (c MeterProviderConfig) getServiceName() string    { return c.ServiceName }
func (c MeterProviderConfig) getServiceVersion() string { return c.ServiceVersion }

func NewMeterProvider(ctx context.Context, cfg MeterProviderConfig) (*sdkmetric.MeterProvider, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultOTELServiceName
	}

	exporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}

	res, err := NewTelemetryResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
	)

	otel.SetMeterProvider(mp)

	return mp, nil
}
