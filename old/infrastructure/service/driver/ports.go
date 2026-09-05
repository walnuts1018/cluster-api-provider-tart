package driver

import (
	"context"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
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

type BootStateObserver interface {
	ObserveBootState(context.Context, driverdomain.HostTarget) (driverdomain.BootState, error)
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
