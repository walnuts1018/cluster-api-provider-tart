package telemetry

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeTracerProviderConfigDefaultsServiceName(t *testing.T) {
	cfg := normalizeTracerProviderConfig(TracerProviderConfig{})

	if cfg.ServiceName != defaultOTELServiceName {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, defaultOTELServiceName)
	}
}

func TestNewTelemetryResourceUsesStandardSDKName(t *testing.T) {
	res, err := newTelemetryResource(context.Background(), TracerProviderConfig{
		ServiceName:    "tart-test",
		ServiceVersion: "test",
	})
	if err != nil {
		t.Fatalf("newTelemetryResource() error = %v", err)
	}

	attrs := make(map[string]string)
	for _, attr := range res.Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}

	if attrs["service.name"] != "tart-test" {
		t.Fatalf("service.name = %q, want tart-test", attrs["service.name"])
	}
	if attrs["telemetry.sdk.name"] != "opentelemetry" {
		t.Fatalf("telemetry.sdk.name = %q, want opentelemetry", attrs["telemetry.sdk.name"])
	}
}

func TestDefaultTraceSamplerIsParentBasedAndConfigurableByEnv(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")

	sampler := defaultTraceSampler()
	description := sampler.Description()

	if !strings.Contains(description, "ParentBased") {
		t.Fatalf("sampler description = %q, want ParentBased sampler", description)
	}
	if !strings.Contains(description, "TraceIDRatioBased{0.25}") {
		t.Fatalf("sampler description = %q, want OTEL_TRACES_SAMPLER_ARG to be used", description)
	}
}

func TestDefaultTraceSamplerFallsBackToTraceIDRatio(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")

	sampler := defaultTraceSampler()
	description := sampler.Description()

	if !strings.Contains(description, "ParentBased") {
		t.Fatalf("sampler description = %q, want ParentBased sampler", description)
	}
	if !strings.Contains(description, "TraceIDRatioBased{0.1}") {
		t.Fatalf("sampler description = %q, want default trace sample rate", description)
	}
}
