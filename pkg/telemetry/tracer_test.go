package telemetry

import (
	"context"
	"testing"
)

func TestNormalizeTracerProviderConfigDefaultsServiceName(t *testing.T) {
	cfg := normalizeTracerProviderConfig(TracerProviderConfig{})

	if cfg.ServiceName != defaultOTELServiceName {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, defaultOTELServiceName)
	}
}

func TestNewTelemetryResourceUsesStandardSDKName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
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
