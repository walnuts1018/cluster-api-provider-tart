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

package port

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type PowerOnService interface {
	PowerOn(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		operationdomain.ID,
		applicationdriver.Invocation,
	) error
}

type BootPreparationService interface {
	PrepareBoot(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		operationdomain.ID,
		*driverdomain.BootTarget,
		applicationdriver.Invocation,
	) (driverdomain.BootTarget, error)
}

type HostPhaseService interface {
	MarkHostProvisioning(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostUpdating(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostProvisioned(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostRecoveryRequired(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostAvailable(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostCleaningForDeletion(
		ctx context.Context,
		host *infrastructurev1beta1.TartHost,
		deletionPolicy infrastructurev1beta1.DeletionPolicy,
	) error
	MarkHostRetained(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	MarkHostDetached(ctx context.Context, host *infrastructurev1beta1.TartHost) error
}

type DriverTargetBuilder interface {
	Build(context.Context, *infrastructurev1beta1.TartHost) (driverdomain.HostTarget, error)
}

type DriverCapabilityObserver interface {
	ObserveAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}

type DriverPowerStateObserver interface {
	ObserveAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}

type DriverBootStateObserver interface {
	ObserveBootAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}
