package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var Meter = otel.Meter("github.com/walnuts1018/cluster-api-provider-tart")

type MeterProviderConfig struct {
	ServiceName    string
	ServiceVersion string
}

func (c MeterProviderConfig) getServiceName() string    { return c.ServiceName }
func (c MeterProviderConfig) getServiceVersion() string { return c.ServiceVersion }

type MeterProvider struct {
	metric.MeterProvider
}

func (t MeterProvider) Shutdown(ctx context.Context) error {
	if tp, ok := t.MeterProvider.(*sdkmetric.MeterProvider); ok {
		return tp.Shutdown(ctx)
	}
	return nil
}

func NewMeterProvider(ctx context.Context, cfg MeterProviderConfig) (MeterProvider, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultOTELServiceName
	}

	res, err := NewTelemetryResource(ctx, cfg)
	if err != nil {
		return MeterProvider{}, fmt.Errorf("failed to create resource: %w", err)
	}

	var mp metric.MeterProvider
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != "" {
		exporter, err := otlpmetricgrpc.New(ctx)
		if err != nil {
			return MeterProvider{}, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
		}
		opts := []sdkmetric.Option{
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		}
		mp = sdkmetric.NewMeterProvider(opts...)
	} else {
		mp = noop.NewMeterProvider()
		fmt.Fprintln(os.Stderr, "OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_METRICS_ENDPOINT is not set, using NoopMeterProvider")
	}

	otel.SetMeterProvider(mp)

	return MeterProvider{MeterProvider: mp}, nil
}
