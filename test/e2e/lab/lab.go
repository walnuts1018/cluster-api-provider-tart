//go:build e2e

// Package labは、Tart E2EのためのQEMU/libvirt製bare-metal labを構築・破棄するヘルパーである。
// 実際のlibvirt操作(cgo依存)はlinux専用ビルドタグを持つlibvirtlab.goが担い、
// このファイルはlinux以外(darwinでの`go vet -tags e2e`検証など)でも共通に使える
// 公開APIの型定義だけを持つ。
package lab

import (
	"context"
)

// VMSpecはlab上に定義する1台のVM(bare-metal相当のHost)を表す。
type VMSpec struct {
	// Nameはlibvirt domain名である。
	Name string
	// MACAddressはVMの主NICに固定するMACアドレスである。CLAUDE.mdの規約に従い
	// 00-00-5E-00-53-00から00-00-5E-00-53-FFの範囲(RFC 7042 IANA予約block)を使う。
	MACAddress string
	// StaticIPは、lab networkのDHCPがこのMACAddressへ常に払い出す固定IPである。
	// TartHost discoveryはTalos maintenance APIへ最初に接続するためのendpointを必要とするが、
	// 初回はinventory観測前でendpointが未知という鶏卵問題があるため、DHCP static reservationで
	// 既知のIPを固定し、TartHost.spec.talosAPIAddressへ明示設定することで解決する。
	StaticIP string
	// VCPUsはVMに割り当てるvCPU数である。
	VCPUs uint
	// MemoryMiBはVMに割り当てるメモリ量(MiB)である。
	MemoryMiB uint64
	// SystemDiskGiBはTalos installの対象となるsystem disk(OS領域)のサイズである。
	SystemDiskGiB uint64
	// SSDDiskGiBは高速data disk相当(UserVolume用)のサイズである。
	SSDDiskGiB uint64
	// HDDDiskGiBは低速data disk相当(UserVolume用)のサイズである。
	HDDDiskGiB uint64
}

// Configはlab全体の設定である。
type Config struct {
	// NetworkNameはVMを接続するlibvirt isolated networkの名前である。
	NetworkName string
	// NetworkBridgeはNetworkNameに対応するbridge interface名である。
	NetworkBridge string
	// NetworkCIDRはlab network(dnsmasqによる通常DHCP付き)のCIDRである。
	// RFC 5737で予約されたTEST-NET rangeを使う。
	NetworkCIDR string
	// WorkDirはdisk image等の作業ファイルを置くディレクトリである。
	WorkDir string
	// VMsはlabで管理するVMの一覧である。
	VMs []VMSpec
}

// DiskPathsは1台のVMに紐づくdisk image file pathを保持する。
type DiskPaths struct {
	System string
	SSD    string
	HDD    string
}

// Labはbare-metal lab全体のライフサイクルを管理するインターフェースである。
// linux実装はlibvirtlab.goのlibvirtLabが提供し、darwin等では常にerrorを返すstub実装を使う
// (libvirt.org/go/libvirtはcgoでlibvirt-devへリンクするため、linuxのlab runner以外では
// ビルド・実行できない)。
type Lab interface {
	// EnsureNetworkはisolated networkとdnsmasqによる通常DHCPをidempotentに用意する。
	EnsureNetwork(ctx context.Context) error
	// EnsureVMはVMSpecに従いdisk imageとdomainをidempotentに定義する(電源はshutoff状態のまま)。
	EnsureVM(ctx context.Context, spec VMSpec) (DiskPaths, error)
	// PowerOffは指定VMを強制停止する(destroy相当)。既に停止していれば成功として扱う。
	PowerOff(ctx context.Context, name string) error
	// IsRunningは指定VMが稼働中かを返す。
	IsRunning(ctx context.Context, name string) (bool, error)
	// DestroyAllはlabが作成した全domain/diskとnetworkを削除する。
	DestroyAll(ctx context.Context) error
	// Closeはlibvirt接続を閉じる。
	Close() error
}
