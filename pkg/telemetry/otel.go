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
	"context"
	"fmt"
)

type Provider struct {
	TracerProvider TraceProvider
	MeterProvider  MeterProvider
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

	if err := p.TracerProvider.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to shutdown tracer provider: %w", err))
	}

	if err := p.MeterProvider.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to shutdown meter provider: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}
