// Package bootstrapは、Talos machine configuration合成とBootstrap Secret contractのusecaseを提供する。
// domain/bootstrapの値オブジェクト・純粋関数をオーケストレーションし、siderolabs machinery型への
// 実際の変換はConfigRenderer port経由でadapter/talos/configbuilderへ委譲する。
package bootstrap

import (
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

// MachineConfigurationContextはclusterとCAPI Machineから導出したTalos設定生成の入力である。
// SecretsBundleはTalos PKI/tokenを保持するsecrets.Bundleを不透明な値として運ぶ。usecase層は
// その具象型を解釈せず、ConfigRenderer実装(adapter/talos/configbuilder)だけが型を確定させる。
type MachineConfigurationContext struct {
	ClusterName          string
	ControlPlaneEndpoint string
	KubernetesVersion    string
	MachineRole          domainbootstrap.MachineRole
	SecretsBundle        any
	// InstallDiskはmaintenance inventoryから選択したinstall対象である。nilの場合はuser patchまたは
	// complete configurationがinstall targetを含まなければならない。
	InstallDisk *domainbootstrap.InstallDisk
}

// ConfigRendererは、domain/bootstrapが表現する合成順序の意思決定を実際のTalos machine configuration
// byte列へ変換するportである。実装はsiderolabs machinery型への変換を全て自身に閉じ込める。
type ConfigRenderer interface {
	// Renderは、完全なbase configurationへraw patchを順番に適用したcanonical configurationを返す。
	Render(base []byte, patches ...[]byte) ([]byte, error)
	// Generateは、Talos machineryのbase configurationを生成し、raw patchとprovider-owned install
	// diskを適用したcanonical configurationを返す。
	Generate(input MachineConfigurationContext, patches ...[]byte) ([]byte, error)
	// ValidateProviderOwnedは、configurationが同じclusterから生成したprovider-owned基準
	// (PKI、token、cluster identity、Kubernetesコンポーネントimage)と一致するかを検証する。
	ValidateProviderOwned(configuration []byte, input MachineConfigurationContext) error
	// Validateは、configurationがTalos client-side validationを満たすかを検証する。
	Validate(configuration []byte) error
	// Digestは、redaction済みcanonical configurationのSHA-256 digestを返す。
	Digest(completeConfiguration []byte) (string, error)
	// HasInstallDiskは、configurationが既にinstall targetを含むかを判定する。
	HasInstallDisk(configuration []byte) (bool, error)
	// EnsureInstallDiskは、install targetを含まないconfigurationへprovider-owned install diskを追加する。
	EnsureInstallDisk(configuration []byte, disk domainbootstrap.InstallDisk) ([]byte, error)
	// SelectInstallDiskは、観測したdisk群から一意に識別できるinstall対象を選択する。
	SelectInstallDisk(disks []domainbootstrap.InstallDisk) (domainbootstrap.InstallDisk, error)
}
