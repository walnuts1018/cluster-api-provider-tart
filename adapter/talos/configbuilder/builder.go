package configbuilder

import (
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
	usecaseupdate "github.com/walnuts1018/cluster-api-provider-tart/usecase/update"
)

// Builderはusecase/bootstrap.ConfigRendererとusecase/update.ConfigDiffClassifierを
// siderolabs machineryで実装する。状態を持たないため、zero valueのまま利用できる。
type Builder struct{}

var (
	_ usecasebootstrap.ConfigRenderer    = Builder{}
	_ usecaseupdate.ConfigDiffClassifier = Builder{}
)

func NewBuilder() Builder {
	return Builder{}
}

func (Builder) Generate(input usecasebootstrap.MachineConfigurationContext, patches ...[]byte) ([]byte, error) {
	return GenerateMachineConfiguration(input, patches...)
}

func (Builder) Digest(completeConfiguration []byte) (string, error) {
	return DigestEffectiveConfiguration(completeConfiguration)
}

func (Builder) SelectDisk(disks []domainbootstrap.DiskIdentity) (domainbootstrap.DiskIdentity, error) {
	return domainbootstrap.SelectDisk(disks)
}

func (Builder) ClassifyConfigurationChange(active, desired []byte) (domainupdate.ChangeClass, string, error) {
	return ClassifyConfigurationChange(active, desired)
}
