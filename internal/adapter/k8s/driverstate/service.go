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

package driverstate

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
)

type PowerStateObserver interface {
	ObservePowerState(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		applicationdriver.Invocation,
	) (driverdomain.PowerState, error)
}

type BootStateObserver interface {
	ObserveBootState(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		applicationdriver.Invocation,
	) (driverdomain.BootState, error)
}

type HostPowerStateWriter interface {
	UpdatePowerState(context.Context, *infrastructurev1beta1.TartHost, infrastructurev1beta1.PowerState) error
	UpdateBootState(context.Context, *infrastructurev1beta1.TartHost, infrastructurev1beta1.BootStateStatus) error
}

// Service はdriverから観測したPowerStateとboot stateをHost Statusへ保存する。
type Service struct {
	powerObserver PowerStateObserver
	bootObserver  BootStateObserver
	writer        HostPowerStateWriter
}

func NewService(powerObserver PowerStateObserver, bootObserver BootStateObserver, writer HostPowerStateWriter) *Service {
	return &Service{
		powerObserver: powerObserver,
		bootObserver:  bootObserver,
		writer:        writer,
	}
}

func (service *Service) ObserveAndPersist(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	host *infrastructurev1beta1.TartHost,
	invocation applicationdriver.Invocation,
) error {
	state, err := service.powerObserver.ObservePowerState(ctx, name, target, invocation)
	if err != nil {
		if driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
			return nil
		}
		return err
	}
	return service.writer.UpdatePowerState(ctx, host, infrastructurev1beta1.PowerState(state))
}

func (service *Service) ObserveBootAndPersist(
	ctx context.Context,
	name driverdomain.Name,
	target driverdomain.HostTarget,
	host *infrastructurev1beta1.TartHost,
	invocation applicationdriver.Invocation,
) error {
	state, err := service.bootObserver.ObserveBootState(ctx, name, target, invocation)
	if err != nil {
		if driverdomain.IsErrorKind(err, driverdomain.ErrorUnsupported) {
			return nil
		}
		return err
	}
	return service.writer.UpdateBootState(ctx, host, infrastructurev1beta1.BootStateStatus{
		OverrideEnabled: state.OverrideEnabled,
		OverrideTarget:  infrastructurev1beta1.BootTarget(state.OverrideTarget),
		VirtualMedia: infrastructurev1beta1.VirtualMediaStatus{
			Inserted:    state.MediaInserted,
			Image:       state.MediaImage,
			OperationID: state.MediaOperation,
		},
	})
}
