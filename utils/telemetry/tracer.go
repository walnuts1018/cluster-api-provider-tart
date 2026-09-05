package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

const (
	defaultOTELServiceName = "cluster-api-provider-tart"
)

var Tracer = otel.Tracer("github.com/walnuts1018/cluster-api-provider-tart")

type TracerProviderConfig struct {
	ServiceName    string
	ServiceVersion string
}

type TraceProvider struct {
	trace.TracerProvider
}

func (t TraceProvider) Shutdown(ctx context.Context) error {
	if tp, ok := t.TracerProvider.(*sdktrace.TracerProvider); ok {
		return tp.Shutdown(ctx)
	}
	return nil
}

func NewTracerProvider(ctx context.Context, cfg TracerProviderConfig) (TraceProvider, error) {
	cfg = normalizeTracerProviderConfig(cfg)

	res, err := newTelemetryResource(ctx, cfg)
	if err != nil {
		return TraceProvider{}, fmt.Errorf("failed to create resource: %w", err)
	}

	var tp trace.TracerProvider
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" {
		exporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return TraceProvider{}, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}

		opts := []sdktrace.TracerProviderOption{
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(exporter),
		}
		tp = sdktrace.NewTracerProvider(opts...)
	} else {
		tp = noop.NewTracerProvider()
		fmt.Fprintln(os.Stderr, "OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT is not set, using NoopTracerProvider")
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return TraceProvider{TracerProvider: tp}, nil
}

func normalizeTracerProviderConfig(cfg TracerProviderConfig) TracerProviderConfig {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultOTELServiceName
	}
	return cfg
}

type telemetryResourceConfig interface {
	getServiceName() string
	getServiceVersion() string
}

func (c TracerProviderConfig) getServiceName() string    { return c.ServiceName }
func (c TracerProviderConfig) getServiceVersion() string { return c.ServiceVersion }

func newTelemetryResource(ctx context.Context, cfg TracerProviderConfig) (*resource.Resource, error) {
	return NewTelemetryResource(ctx, cfg)
}

func NewTelemetryResource(ctx context.Context, cfg telemetryResourceConfig) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.getServiceName()),
			semconv.ServiceVersion(cfg.getServiceVersion()),
		),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithHost(),
	)
}
