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

package drivercapability

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
)

type CapabilityDiscoverer interface {
	DiscoverCapabilities(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		applicationdriver.Invocation,
	) (capabilitydomain.Set, error)
}

type HostCapabilityWriter interface {
	UpdateCapabilities(context.Context, *infrastructurev1beta1.TartHost, capabilitydomain.Set) error
}

// Service はdriver capability discovery結果をHost Statusへ保存する。
type Service struct {
	discoverer CapabilityDiscoverer
	writer     HostCapabilityWriter
}

func NewService(discoverer CapabilityDiscoverer, writer HostCapabilityWriter) *Service {
	return &Service{
		discoverer: discoverer,
		writer:     writer,
	}
}

func (service *Service) ObserveAndPersist(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	host *infrastructurev1beta1.TartHost,
	invocation applicationdriver.Invocation,
) error {
	capabilities, err := service.discoverer.DiscoverCapabilities(ctx, name, target, invocation)
	if err != nil {
		return err
	}
	return service.writer.UpdateCapabilities(ctx, host, capabilities)
}
