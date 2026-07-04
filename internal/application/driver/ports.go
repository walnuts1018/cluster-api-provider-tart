package driver

import (
	"context"

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
