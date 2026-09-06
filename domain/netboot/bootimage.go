// Package netbootは、PXE bootしてきたクライアントへ配信すべきTalos imageやbootファイルを
// 決定する純粋なロジックだけを持つ。DHCP/TFTP/HTTPのプロトコル実装やKubernetes API参照は
// adapter/netboot配下が担い、このpackageは外部依存を持たない値オブジェクトと純粋関数のみで構成する。
package netboot

import (
	"context"
	"strings"
)

// BootImageは、あるMACアドレスからのPXE bootリクエストに対して配信すべきTalos imageの解決結果である。
type BootImage struct {
	// Versionは"v"始まりのTalos versionである。
	Version string
	// SchematicIDはTalos Image Factoryのschematic identifierである。
	SchematicID string
}

// HostImageResolverは、MACアドレスからPXEクライアントへ配信すべきTalos imageを解決する。
type HostImageResolver interface {
	// ResolveBootImageは指定のMACアドレスに対応するBootImageを返す。
	// このMACアドレスに対応するTartHostが存在しないか、TartHostがまだ稼働中のMachineへ
	// claimされていない場合はfoundがfalseになり、呼び出し側はdiscovery用の既定imageへfallbackする。
	ResolveBootImage(ctx context.Context, mac string) (image BootImage, found bool, err error)
}

// NoopHostImageResolverは、Kubernetes APIへの接続が設定されていない場合に使う既定のresolverであり、
// 常にfound=falseを返してdiscovery用imageへfallbackさせる。
type NoopHostImageResolver struct{}

// ResolveBootImageは常にfound=falseを返す。
func (NoopHostImageResolver) ResolveBootImage(context.Context, string) (BootImage, bool, error) {
	return BootImage{}, false, nil
}

// DiscoveryImageはPXE bootした素のhostをTalos maintenance modeへ到達させるためのdesired Talos imageである。
// TartMachine個別のinstaller image(spec.talosImageの{version, schematicID})とは別物であり、
// operatorがnetboot-serverの設定として指定するdiscovery専用の値を保持する。
// このimageはTartHost/TartMachineへclaimされる前の初回enrollment bootでのみ使われるため、
// 未設定のままでもnetboot-server自体は起動し、既にTartHost/TartMachineへclaimされたHostの
// PXE bootは通常通り解決できる。未設定のまま初回enrollment bootを行うhostへは、
// discovery先が不明であることを示すiPXEスクリプトを返す。
type DiscoveryImage struct {
	// Versionは"v"始まりのTalos version(例: v1.11.2)である。空の場合は未設定として扱う。
	Version string
	// SchematicIDはTalos Image Factoryのschematic identifierである。空の場合は未設定として扱う。
	SchematicID string
}

// IsZeroはDiscoveryImageが未設定かを返す。
func (image DiscoveryImage) IsZero() bool {
	return strings.TrimSpace(image.Version) == "" || strings.TrimSpace(image.SchematicID) == ""
}

// Archはクライアントのアーキテクチャを表す(DHCP Option 93: Client System Architecture Typeの値)。
type Arch uint16

const (
	ArchIntelx86PC Arch = 0
	ArchEFIx8664   Arch = 7
	ArchEFIBC      Arch = 9
	ArchEFIARM64   Arch = 11
)

const (
	// IPXEBootFileNameAMD64はamd64用のiPXEローダのファイル名である。
	IPXEBootFileNameAMD64 = "ipxe-x86_64.efi"
	// IPXEBootFileNameARM64はarm64用のiPXEローダのファイル名である。
	IPXEBootFileNameARM64 = "ipxe-arm64.efi"
)

// DecideAgentBootFileは、DHCP requestから読み取ったクライアントのarchitectureとiPXE状態を基に、
// ProxyDHCPが応答すべきboot file名(またはchain URL)を決定する。
// Option 93(Client System Architecture)がない、またはamd64 EFI以外のarchitectureはsupported=falseとなり、
// 呼び出し側はそのクライアントへの応答を送らない(対象外のhostへブートローダを配信してしまうことを防ぐため)。
// isIPXEがfalseの場合はまずiPXEローダのファイル名を返し、iPXEローダ自身からの2回目のrequest(isIPXE=true)では
// macアドレスをクエリパラメータへ含めたHTTP boot script URLへchainさせる。
func DecideAgentBootFile(arch Arch, archOptionPresent, isIPXE bool, httpBootBaseURL, macAddress string) (bootFile string, supported bool) {
	if !archOptionPresent || arch != ArchEFIx8664 {
		return "", false
	}
	if !isIPXE {
		return IPXEBootFileNameAMD64, true
	}
	return httpBootBaseURL + "/ipxe?mac=" + macAddress, true
}

// PXEArchFromQueryは、iPXEスクリプト配信endpointへのクエリパラメータから、
// Talos Image FactoryのPXE配信endpointが要求するarch文字列を決定する。
// 現時点でDHCP側はamd64のみをiPXEブートローダへ誘導するため既定値はamd64だが、
// 将来arm64クライアントにも対応できるようクエリで上書きできるようにしておく。
func PXEArchFromQuery(arch string) string {
	switch strings.ToLower(arch) {
	case "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}
