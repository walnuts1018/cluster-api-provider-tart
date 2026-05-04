package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

const (
	defaultOTELServiceName = "cluster-api-provider-tart"
	defaultTraceSampleRate = 0.1
)

var Tracer = otel.Tracer("github.com/walnuts1018/cluster-api-provider-tart")

type TracerProviderConfig struct {
	ServiceName    string
	ServiceVersion string
}

func NewTracerProvider(ctx context.Context, cfg TracerProviderConfig) (*sdktrace.TracerProvider, error) {
	cfg = normalizeTracerProviderConfig(cfg)

	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	res, err := newTelemetryResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}

func normalizeTracerProviderConfig(cfg TracerProviderConfig) TracerProviderConfig {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultOTELServiceName
	}
	return cfg
}

func newTelemetryResource(ctx context.Context, cfg TracerProviderConfig) (*resource.Resource, error) {
	cfg = normalizeTracerProviderConfig(cfg)
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithHost(),
	)
}
