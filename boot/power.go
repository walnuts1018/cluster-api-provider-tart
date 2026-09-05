// Package bootは最小限のmaintenance bootおよび電源機能のインターフェースと、Wake-on-LANなどの具体的なバックエンドを提供する。電源とbootは固定のDHCP/TFTP/PXE実装ではなく、TartHostの機能として扱う。詳細は.agents/skills/host-lifecycle/SKILL.mdを参照する。
package boot

import "context"

// PowerStateは電源backendが観測したHostの電源状態である。
type PowerState string

const (
	PowerStateOn      PowerState = "On"
	PowerStateOff     PowerState = "Off"
	PowerStateUnknown PowerState = "Unknown"
)

// PowerOnはHostの電源投入を要求する。成功は要求が受理されたことだけを示し、maintenance Talosの起動やインストールの成功を示さない。呼び出し側はHostをprovisionedとみなす前にmaintenanceまたはauthenticated Talos APIを別途観測する。
type PowerOn interface {
	PowerOn(ctx context.Context) error
}

// PowerOffはHostの安全な電源停止を要求する。呼び出し側は停止要求の受理だけで停止完了とみなさず、PowerStateを別途観測する。
type PowerOff interface {
	PowerOff(ctx context.Context) error
}

// PowerStateObserverはHostの電源状態を観測する。通信エラーや未対応状態はPowerStateUnknownではなくerrorとして返す実装も許容するが、呼び出し側はOff以外を停止完了とみなしてはならない。
type PowerStateObserver interface {
	PowerState(ctx context.Context) (PowerState, error)
}
