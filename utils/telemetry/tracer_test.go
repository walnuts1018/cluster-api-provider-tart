// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package telemetry

import (
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
	res, err := newTelemetryResource(t.Context(), TracerProviderConfig{
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
