package configbuilder

import (
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
)

// Builderはusecase/bootstrap.ConfigRendererをsiderolabs machineryで実装する。
// 状態を持たないため、zero valueのまま利用できる。
type Builder struct{}

var _ usecasebootstrap.ConfigRenderer = Builder{}

func (Builder) Render(base []byte, patches ...[]byte) ([]byte, error) {
	return RenderEffectiveConfiguration(base, patches...)
}

func (Builder) Generate(input usecasebootstrap.MachineConfigurationContext, patches ...[]byte) ([]byte, error) {
	return GenerateMachineConfiguration(input, patches...)
}

func (Builder) ValidateProviderOwned(configuration []byte, input usecasebootstrap.MachineConfigurationContext) error {
	return ValidateProviderOwnedConfiguration(configuration, input)
}

func (Builder) Validate(configuration []byte) error {
	return ValidateMachineConfiguration(configuration)
}

func (Builder) Digest(completeConfiguration []byte) (string, error) {
	return DigestEffectiveConfiguration(completeConfiguration)
}

func (Builder) HasInstallDisk(configuration []byte) (bool, error) {
	return HasInstallDiskConfiguration(configuration)
}

func (Builder) EnsureInstallDisk(configuration []byte, disk domainbootstrap.InstallDisk) ([]byte, error) {
	return EnsureInstallDisk(configuration, disk)
}

func (Builder) SelectInstallDisk(disks []domainbootstrap.InstallDisk) (domainbootstrap.InstallDisk, error) {
	return SelectInstallDisk(disks)
}
