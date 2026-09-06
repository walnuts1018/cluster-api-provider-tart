package bootstrap

import (
	corev1 "k8s.io/api/core/v1"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

// RenderFromCompleteValueは、ユーザーが提供したcomplete Talos machine configuration inputを
// provider-owned contextに対して検証し、install targetが無ければ付加した上でTalos client-side
// validationを通過したconfigurationを返す。
func RenderFromCompleteValue(renderer ConfigRenderer, secret *corev1.Secret, ctx MachineConfigurationContext) ([]byte, error) {
	configuration, err := CompleteConfigurationFromSecret(renderer, secret)
	if err != nil {
		return nil, err
	}

	configured, err := renderer.HasInstallDisk(configuration)
	if err != nil {
		return nil, err
	}
	if !configured {
		if ctx.InstallDisk == nil {
			return nil, domainbootstrap.ErrInstallDiskUnavailable
		}
		configuration, err = renderer.EnsureInstallDisk(configuration, *ctx.InstallDisk)
		if err != nil {
			return nil, err
		}
	}

	if err := renderer.ValidateProviderOwned(configuration, ctx); err != nil {
		return nil, err
	}
	if err := renderer.Validate(configuration); err != nil {
		return nil, err
	}
	return configuration, nil
}

// RenderFromPatchesは、provider-owned base configurationへraw patchを適用したcomplete machine
// configurationを生成する。patchesが空の場合はbase configurationのみから生成する。
func RenderFromPatches(renderer ConfigRenderer, ctx MachineConfigurationContext, patches []byte) ([]byte, error) {
	if len(patches) == 0 {
		return renderer.Generate(ctx)
	}
	return renderer.Generate(ctx, patches)
}
