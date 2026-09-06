// Package recoveryはTalos recovery identity照合の純粋なpolicyを提供する。
// 外部依存はdomain/networkのみであり、Kubernetes API型やTalos machinery型は一切扱わない。
package recovery

import (
	"errors"
	"slices"
	"strings"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

var (
	// ErrEndpointUnknownはHost inventoryと照合できるTalos endpointが存在しないことを示す。
	ErrEndpointUnknown = errors.New("talos endpoint does not match the TartHost inventory")
	// ErrClusterIdentityMismatchは接続先が承認された旧Talos clusterへ属さないことを示す。
	ErrClusterIdentityMismatch = errors.New("talos cluster identity does not match the approved recovery identity")
	// ErrHostIdentityMismatchは接続先が承認された旧installを保持するHostではないことを示す。
	ErrHostIdentityMismatch = errors.New("talos machine identity does not match the claimed TartHost")
	// ErrHostIdentityUnverifiableはHost identityを積極的に確認できるevidenceが揃わないことを示す。
	ErrHostIdentityUnverifiable = errors.New("talos machine identity could not be verified")
)

// ExpectedIdentityはReset対象として承認されたHostとTalos clusterのidentityである。
type ExpectedIdentity struct {
	// ClusterIDはrecovery Secretが表す旧Talos cluster IDである。
	ClusterID string
	// MACAddressはTartHostのenrollment identityである。
	MACAddress network.MACAddress
	// SystemUUIDは直近に観測したHost inventoryのsystem UUIDである。観測できていない場合は空になる。
	SystemUUID string
	// EndpointはTartHost inventoryから解決したTalos APIの接続先である。
	Endpoint string
}

// ObservedIdentityは認証済みTalos APIから観測したidentityである。
type ObservedIdentity struct {
	// ClusterIDは対象nodeのactive machine configurationから導出したTalos cluster IDである。
	ClusterID string
	// MACAddressesは対象nodeが報告した物理linkのMAC addressである。
	MACAddresses []network.MACAddress
	// SystemUUIDは対象nodeが報告したsystem UUIDである。
	SystemUUID string
	// Endpointは実際に接続したTalos APIのendpointである。
	Endpoint string
}

// VerifyResetTargetはTalos Resetのようにデータを不可逆に破棄する操作の直前に、対象が承認済みのHostかつ承認済みの旧Talos clusterであることを確認する。
// TLS認証(recovery CAによるserver certificate検証とclient certificate提示)が成功していることは呼び出し側の前提であり、この関数はその上で観測したidentityの一致だけを判定する。
// MAC addressやIP addressの一致だけを根拠にせず、cluster identityとmachine identityの双方が一致しない限りfail-closedでerrorを返す。
func VerifyResetTarget(expected ExpectedIdentity, observed ObservedIdentity) error {
	if strings.TrimSpace(expected.ClusterID) == "" {
		return ErrClusterIdentityMismatch
	}
	if strings.TrimSpace(observed.ClusterID) == "" || observed.ClusterID != expected.ClusterID {
		return ErrClusterIdentityMismatch
	}
	if strings.TrimSpace(expected.Endpoint) == "" || expected.Endpoint != strings.TrimSpace(observed.Endpoint) {
		return ErrEndpointUnknown
	}
	if expected.MACAddress.IsZero() || !slices.Contains(observed.MACAddresses, expected.MACAddress) {
		return ErrHostIdentityMismatch
	}
	// system UUIDはBIOSでの欠落や重複があり得るため単独の根拠にはしないが、TartHostのinventoryへ記録済みの値と食い違う場合は別Hostの可能性があるため必ず停止する。
	expectedUUID := normalizedUUID(expected.SystemUUID)
	observedUUID := normalizedUUID(observed.SystemUUID)
	if expectedUUID != "" {
		if observedUUID == "" {
			return ErrHostIdentityUnverifiable
		}
		if observedUUID != expectedUUID {
			return ErrHostIdentityMismatch
		}
	}
	return nil
}

// normalizedUUIDは比較のためにsystem UUIDを正規化し、TalosやBIOSがidentityを持たない場合に返す全て0の値を空として扱う。
func normalizedUUID(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "00000000-0000-0000-0000-000000000000" {
		return ""
	}
	return normalized
}
