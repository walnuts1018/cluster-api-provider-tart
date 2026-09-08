// Package bootstrapは、Talos machine configuration合成とBootstrap Secret contractのusecaseを提供する。
// domain/bootstrapの値オブジェクト・純粋関数をオーケストレーションし、siderolabs machinery型への
// 実際の変換はConfigRenderer interface経由でadapter/talos/configbuilderへ委譲する。
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
	// InstallDiskはmaintenance inventoryから選択したinstall対象である。nilの場合はraw patchが
	// install targetを含まなければならない。
	InstallDisk *domainbootstrap.DiskIdentity
	// AllowSchedulingOnControlPlanesは、control plane nodeへの通常Podのschedulingを許可するかを
	// 制御する。Talos 1.14以降はKubernetes関連設定がmultidoc化されており、control planeのNoSchedule
	// taintはgenerate時のoptionでしか外せない(生成後のconfig patchでは信頼できるmergeができない)ため、
	// raw patchではなくこのfield経由でTalos machineryのgenerate optionへ渡す。
	AllowSchedulingOnControlPlanes bool
	// DisableDefaultCNIは、Talosが既定で生成するFlannel CNI(KubeFlannelCNIConfig document)を
	// 生成結果から除外するかを制御する。Talos machineryのCNICustomURLオプションはCNIの無効化と
	// 同時に外部manifestの自動fetch/applyという副作用を持つため使わず、生成後のdocument一覧から
	// KubeFlannelCNIConfigだけを取り除く形で実現する(Cilium等を別途管理する運用を想定する)。
	DisableDefaultCNI bool
}

// ConfigRendererは、domain/bootstrapが表現する合成順序の意思決定を実際のTalos machine configuration
// byte列へ変換するinterfaceである。実装はsiderolabs machinery型への変換を全て自身に閉じ込める。
// TartはTart生成のbase configurationへuser patchを適用したものだけをeffective machine
// configurationとする一方向モデルを取るため、complete configurationを直接受け取る経路は持たない。
type ConfigRenderer interface {
	// Generateは、Talos machineryのbase configurationを生成し、raw patchとprovider-owned install
	// diskを適用したcanonical configurationを返す。Talos client-side validationも内部で行う。
	Generate(input MachineConfigurationContext, patches ...[]byte) ([]byte, error)
	// Digestは、redaction済みcanonical configurationのSHA-256 digestを返す。
	Digest(completeConfiguration []byte) (string, error)
	// SelectDiskは、観測したdisk群から一意に識別できるinstall対象を選択する。
	SelectDisk(disks []domainbootstrap.DiskIdentity) (domainbootstrap.DiskIdentity, error)
}
