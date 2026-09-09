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

type TraceProvider struct {
	trace.TracerProvider
}

func (t TraceProvider) Shutdown(ctx context.Context) error {
	if tp, ok := t.TracerProvider.(*sdktrace.TracerProvider); ok {
		return tp.Shutdown(ctx)
	}
	return nil
}

func NewTracerProvider(ctx context.Context, cfg ResourceConfig) (TraceProvider, error) {
	res, err := NewTelemetryResource(ctx, cfg)
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

// ResourceConfig identifies the service for both the trace and metric providers.
type ResourceConfig struct {
	ServiceName    string
	ServiceVersion string
}

func NewTelemetryResource(ctx context.Context, cfg ResourceConfig) (*resource.Resource, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultOTELServiceName
	}
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
