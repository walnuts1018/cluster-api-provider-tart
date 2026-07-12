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

package inplaceupdate

import (
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

func TestRecordUpdateOutcomeは許可されたLabelだけを出力する(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	originalMeter := telemetry.Meter
	telemetry.Meter = provider.Meter("inplaceupdate-observability-test")
	t.Cleanup(func() {
		telemetry.Meter = originalMeter
		if err := provider.Shutdown(t.Context()); err != nil {
			t.Errorf("shutdown MeterProvider: %v", err)
		}
	})

	operation := &infrastructurev1beta1.TartHostOperation{
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type: infrastructurev1beta1.OperationTypeUpdate,
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseFailed,
		},
	}

	RecordUpdateOutcome(t.Context(), operation, "failed")

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(metrics.ScopeMetrics) != 1 || len(metrics.ScopeMetrics[0].Metrics) != 1 {
		t.Fatalf("metrics = %#v, want one metric", metrics.ScopeMetrics)
	}
	sum, ok := metrics.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("metric data = %#v, want one int64 data point", metrics.ScopeMetrics[0].Metrics[0].Data)
	}

	got := make([]string, 0, sum.DataPoints[0].Attributes.Len())
	for iter := sum.DataPoints[0].Attributes.Iter(); iter.Next(); {
		kv := iter.Attribute()
		got = append(got, string(kv.Key))
	}
	slices.Sort(got)
	want := []string{"operation_type", "phase", "result", "rollback"}
	if !slices.Equal(got, want) {
		t.Fatalf("metric labels = %v, want %v", got, want)
	}
}
