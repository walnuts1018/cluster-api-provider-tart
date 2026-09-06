package bootstrap

// RenderFromPatchesは、provider-owned base configurationへraw patchを適用したcomplete machine
// configurationを生成する。patchesが空の場合はbase configurationのみから生成する。
func RenderFromPatches(renderer ConfigRenderer, ctx MachineConfigurationContext, patches []byte) ([]byte, error) {
	if len(patches) == 0 {
		return renderer.Generate(ctx)
	}
	return renderer.Generate(ctx, patches)
}
