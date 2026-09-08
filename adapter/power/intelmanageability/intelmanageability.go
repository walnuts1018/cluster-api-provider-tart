// Package intelmanageabilityはIntel Standard Manageability/AMTのWS-Management(WS-Man)/CIM経由の電源backendを提供する。
// フルAMT固有機能(KVM、SOL、IDE-R、remote provisioning)には依存せず、DMTFの標準Power State Management profile(CIM_ComputerSystem、CIM_PowerManagementService、CIM_AssociatedPowerManagementService)だけを使う。
// 使用するCIM ResourceURI、selector名、PowerState値の割当はIntel ME 7.1 / Standard Manageability実機での検証結果に基づく。他世代のMEやvProのみの拡張classに依存する変更を行う場合は実機で再検証すること。
package intelmanageability

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
)

// PowerStateは電源backendが観測したHostの電源状態である。adapter/powerの同名型と文字列互換である。
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

// PowerOffはHostの電源停止を要求する。
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

// ErrUnexpectedPowerStateは、要求された電源操作が現在のPowerStateから安全に実行できないことを表す。
var ErrUnexpectedPowerState = errors.New("intel manageability reported an unexpected power state")

const (
	// resourceURIComputerSystem等はDMTF CIM Schemaの標準ResourceURIである。AMT固有namespace(http://intel.com/wbem/wscim/...)は使わず、Standard Manageabilityでも提供される基本profileに留める。
	resourceURIComputerSystem                   = "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ComputerSystem"
	resourceURIPowerManagementService           = "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_PowerManagementService"
	resourceURIAssociatedPowerManagementService = "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_AssociatedPowerManagementService"

	// managedSystemName等はCIM_ComputerSystemインスタンスの慣習的なキー名である。TODO: 実機で異なるNameが返る場合はEnumerateしたCIM_ComputerSystemのNameを動的に読み取るよう変更する。解消条件はOptiPlex 790実機でのWS-Man応答確認。
	managedSystemName          = "ManagedSystem"
	managedSystemCreationClass = "CIM_ComputerSystem"

	// cimPowerState*はDMTF CIM_PowerManagementService::PowerStateの値域(RequestPowerStateChangeの入力兼、CIM_AssociatedPowerManagementService.PowerStateの観測値)である。
	cimPowerStateOn              = "2"
	cimPowerStateOffHard         = "6"
	cimPowerStateOffSoft         = "8"
	cimPowerStateMasterBusReset  = "10"
	cimPowerStateOffSoftGraceful = "12"
	cimPowerStateOffHardGraceful = "13"
)

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
	client wsmanClient
}

var (
	_ PowerOn            = (*Backend)(nil)
	_ PowerOff           = (*Backend)(nil)
	_ PowerCycle         = (*Backend)(nil)
	_ PowerStateObserver = (*Backend)(nil)
)

// wsmanClientはBackendが依存する最小限のWS-Man操作である。protocol実装(client)をtestなしで差し替えられるようにし、unit testはHTTPやXMLを介さずにpower遷移ロジックだけを検証できる。
type wsmanClient interface {
	get(ctx context.Context, resourceURI string) (*xmlNode, error)
	enumerateAll(ctx context.Context, resourceURI string) ([]*xmlNode, error)
	invoke(ctx context.Context, resourceURI, method string, params []invokeParam) (*xmlNode, error)
}

// NewはHTTP(S) endpoint、credential、TLS設定を検証してIntel Manageability backendを構築する。
func New(config Config) (*Backend, error) {
	address := strings.TrimSpace(config.Address)
	if _, err := endpoint.ParseHTTPURL(address); err != nil {
		return nil, fmt.Errorf("validate intel manageability address: %w", err)
	}
	wsClient, err := newClient(clientConfig{
		endpoint:           address,
		username:           config.Username,
		password:           config.Password,
		caData:             config.CAData,
		insecureSkipVerify: config.InsecureSkipVerify,
	})
	if err != nil {
		return nil, err
	}
	return &Backend{client: wsClient}, nil
}

// PowerOnは現在の電源状態を確認し、停止中の場合だけPower On遷移を要求する。
func (b *Backend) PowerOn(ctx context.Context) error {
	state, err := b.PowerState(ctx)
	if err != nil {
		return err
	}
	switch state {
	case PowerStateOn:
		return nil
	case PowerStateOff:
		return b.requestPowerStateChange(ctx, cimPowerStateOn)
	case PowerStateUnknown:
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
	case PowerStateOff:
		return nil
	case PowerStateOn:
		return b.requestPowerStateChange(ctx, cimPowerStateOffSoft)
	case PowerStateUnknown:
	}
	return fmt.Errorf("%w: cannot power off from state %q", ErrUnexpectedPowerState, state)
}

// PowerCycleは稼働中の場合だけhardware resetを要求する。停止中や不明な状態からのcycleは安全でないため拒否する。
func (b *Backend) PowerCycle(ctx context.Context) error {
	state, err := b.PowerState(ctx)
	if err != nil {
		return err
	}
	if state != PowerStateOn {
		return fmt.Errorf("%w: cannot power cycle from state %q", ErrUnexpectedPowerState, state)
	}
	return b.requestPowerStateChange(ctx, cimPowerStateMasterBusReset)
}

// PowerStateはCIM_AssociatedPowerManagementServiceを列挙して現在のPowerStateを観測する。想定外の値やprotocolエラーはUnknownとして扱い、呼び出し側はOnまたはOffとの完全一致だけを根拠にする。
func (b *Backend) PowerState(ctx context.Context) (PowerState, error) {
	instances, err := b.client.enumerateAll(ctx, resourceURIAssociatedPowerManagementService)
	if err != nil {
		return PowerStateUnknown, err
	}
	if len(instances) == 0 {
		return PowerStateUnknown, fmt.Errorf("%w: no CIM_AssociatedPowerManagementService instance was returned", ErrProtocol)
	}
	node := instances[0].find("PowerState")
	if node == nil || strings.TrimSpace(node.Content) == "" {
		return PowerStateUnknown, fmt.Errorf("%w: CIM_AssociatedPowerManagementService instance has no PowerState", ErrProtocol)
	}
	return mapCIMPowerState(strings.TrimSpace(node.Content)), nil
}

func mapCIMPowerState(value string) PowerState {
	switch value {
	case cimPowerStateOn:
		return PowerStateOn
	case cimPowerStateOffHard, cimPowerStateOffSoft, cimPowerStateOffSoftGraceful, cimPowerStateOffHardGraceful:
		return PowerStateOff
	default:
		return PowerStateUnknown
	}
}

func (b *Backend) requestPowerStateChange(ctx context.Context, powerState string) error {
	_, err := b.client.invoke(ctx, resourceURIPowerManagementService, "RequestPowerStateChange", []invokeParam{
		{name: "PowerState", value: powerState},
		{name: "ManagedElement", reference: &endpointReference{
			address:     nsAnonymous,
			resourceURI: resourceURIComputerSystem,
			selectors: []invokeParam{
				{name: "CreationClassName", value: managedSystemCreationClass},
				{name: "Name", value: managedSystemName},
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("request intel manageability power state change to %q: %w", powerState, err)
	}
	return nil
}
