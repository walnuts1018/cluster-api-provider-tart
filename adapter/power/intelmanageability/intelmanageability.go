// Package intelmanageabilityはIntel Standard Manageability/AMTのWS-Management(WS-Man)/CIM経由の電源backendを提供する。
// フルAMT固有機能(KVM、SOL、IDE-R、remote provisioning)には依存せず、DMTFの標準Power State Management profile
// (CIM_PowerManagementService、CIM_AssociatedPowerManagementService)だけを使う。WS-Man protocolそのものは
// github.com/device-management-toolkit/go-wsman-messages/v2(client.go)に委譲し、このfileはHostの電源状態遷移の
// 安全性policyだけを持つ。
package intelmanageability

import (
	"context"
	"errors"
	"fmt"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/power"
)

// ErrUnexpectedPowerStateは、要求された電源操作が現在のPowerStateから安全に実行できないことを表す。
var ErrUnexpectedPowerState = errors.New("intel manageability reported an unexpected power state")

// Configはbackendへ渡す解決済みの接続設定である。Secretの参照名ではなくcredentialの値を受け取り、呼び出し側でSecret値をログやStatusへ出力してはならない。
type Config struct {
	Address            string
	Username           string
	Password           string
	CAData             []byte
	InsecureSkipVerify bool
}

// BackendはIntel Standard Manageability/AMTのWS-Man power controlだけを使う電源backendである。OS停止はTalosへ委譲し、PowerOffは明示的な要求としてのみ提供する。
type Backend struct {
	client manageabilityClient
}

var (
	_ power.PowerOn            = (*Backend)(nil)
	_ power.PowerOff           = (*Backend)(nil)
	_ power.PowerCycle         = (*Backend)(nil)
	_ power.PowerStateObserver = (*Backend)(nil)
)

// NewはHTTP(S) endpoint、credential、TLS設定を検証してIntel Manageability backendを構築する。
func New(config Config) (*Backend, error) {
	client, err := newClient(config)
	if err != nil {
		return nil, err
	}
	return &Backend{client: client}, nil
}

// PowerOnは現在の電源状態を確認し、停止中の場合だけPower On遷移を要求する。
func (b *Backend) PowerOn(ctx context.Context) error {
	state, err := b.PowerState(ctx)
	if err != nil {
		return err
	}
	switch state {
	case power.PowerStateOn:
		return nil
	case power.PowerStateOff:
		return b.requestPowerStateChange(ctx, dmtfPowerOn)
	case power.PowerStateUnknown:
	}
	return fmt.Errorf("%w: cannot power on from state %q", ErrUnexpectedPowerState, state)
}

// PowerOffは現在の電源状態を確認し、稼働中の場合だけACPI経由のsoft power off遷移を要求する。強制停止は自動選択しない。
func (b *Backend) PowerOff(ctx context.Context) error {
	state, err := b.PowerState(ctx)
	if err != nil {
		return err
	}
	switch state {
	case power.PowerStateOff:
		return nil
	case power.PowerStateOn:
		return b.requestPowerStateChange(ctx, dmtfPowerOffSoft)
	case power.PowerStateUnknown:
	}
	return fmt.Errorf("%w: cannot power off from state %q", ErrUnexpectedPowerState, state)
}

// PowerCycleは稼働中の場合だけhardware resetを要求する。停止中や不明な状態からのcycleは安全でないため拒否する。
func (b *Backend) PowerCycle(ctx context.Context) error {
	state, err := b.PowerState(ctx)
	if err != nil {
		return err
	}
	if state != power.PowerStateOn {
		return fmt.Errorf("%w: cannot power cycle from state %q", ErrUnexpectedPowerState, state)
	}
	return b.requestPowerStateChange(ctx, dmtfPowerMasterBusReset)
}

// PowerStateはCIM_AssociatedPowerManagementServiceの現在のPowerStateを観測する。想定外の値やprotocolエラーは
// Unknownとして扱い、呼び出し側はOnまたはOffとの完全一致だけを根拠にする。
func (b *Backend) PowerState(ctx context.Context) (power.PowerState, error) {
	state, err := b.client.currentPowerState(ctx)
	if err != nil {
		return power.PowerStateUnknown, err
	}
	return mapCIMPowerState(state), nil
}

func mapCIMPowerState(state cimPowerState) power.PowerState {
	switch state {
	case dmtfPowerOn:
		return power.PowerStateOn
	case dmtfPowerOffHard, dmtfPowerOffSoft, dmtfPowerOffSoftGraceful, dmtfPowerOffHardGraceful:
		return power.PowerStateOff
	case dmtfPowerMasterBusReset:
		// Master Bus Reset自体は遷移中の一時状態であり、On/Offのどちらとも確定できない。
		return power.PowerStateUnknown
	default:
		return power.PowerStateUnknown
	}
}

func (b *Backend) requestPowerStateChange(ctx context.Context, state cimPowerState) error {
	if err := b.client.requestPowerStateChange(ctx, state); err != nil {
		return fmt.Errorf("request intel manageability power state change to %d: %w", state, err)
	}
	return nil
}
