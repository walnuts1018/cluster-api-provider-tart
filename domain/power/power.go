// Package powerはHostの電源backendが満たす共通契約を定義する。
// adapter/power配下の各backend実装(wol、redfish、intelmanageability等)はこのpackageの型を直接使うことで、
// 電源状態の意味論(PowerStateの値域とPowerOn/PowerOff/PowerCycleの意味)を実装ごとに再定義せず一箇所に集約する。
package power

import "context"

// PowerStateは電源backendが観測したHostの電源状態である。
type PowerState string

const (
	PowerStateOn      PowerState = "On"
	PowerStateOff     PowerState = "Off"
	PowerStateUnknown PowerState = "Unknown"
)

// PowerOnはHostの電源投入を要求する。
type PowerOn interface {
	PowerOn(ctx context.Context) error
}

// PowerOffはHostの安全な電源停止を要求する。
type PowerOff interface {
	PowerOff(ctx context.Context) error
}

// PowerCycleはHostの電源再投入(reset)を要求する。
type PowerCycle interface {
	PowerCycle(ctx context.Context) error
}

// PowerStateObserverはHostの電源状態を観測する。
type PowerStateObserver interface {
	PowerState(ctx context.Context) (PowerState, error)
}
