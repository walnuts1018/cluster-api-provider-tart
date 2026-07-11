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

package driver

import (
	"context"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type PowerOnDriver interface {
	PowerOn(context.Context, driverdomain.HostTarget, operationdomain.ID) error
}

type PowerOffDriver interface {
	PowerOff(context.Context, driverdomain.HostTarget, operationdomain.ID) error
}

type PowerStateObserver interface {
	ObservePowerState(context.Context, driverdomain.HostTarget) (driverdomain.PowerState, error)
}

type BootOverrideDriver interface {
	SetNextBoot(context.Context, driverdomain.HostTarget, driverdomain.BootTarget, operationdomain.ID) error
}

type VirtualMediaDriver interface {
	Mount(context.Context, driverdomain.HostTarget, driverdomain.Artifact, operationdomain.ID) error
	Unmount(context.Context, driverdomain.HostTarget, operationdomain.ID) error
}

type AgentArtifactProvider interface {
	VirtualMediaArtifact(context.Context, operationdomain.ID) (driverdomain.Artifact, error)
}

type CapabilityDiscoverer interface {
	DiscoverCapabilities(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		Invocation,
	) (capabilitydomain.Set, error)
}
