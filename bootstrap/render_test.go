package bootstrap

import (
	"bytes"
	"errors"
	"testing"
)

const renderBaseConfiguration = `version: v1alpha1
machine:
  type: worker
  token: token-a
`

func TestRenderEffectiveConfiguration(t *testing.T) {
	t.Parallel()

	strategicPatch := []byte(`machine:
  certSANs:
    - 192.0.2.10
`)
	jsonPatch := []byte(`[{"op":"add","path":"/machine/certSANs/-","value":"192.0.2.11"}]`)

	withoutPatches, err := RenderEffectiveConfiguration([]byte(renderBaseConfiguration))
	if err != nil {
		t.Fatalf("RenderEffectiveConfiguration() error = %v", err)
	}
	withPatches, err := RenderEffectiveConfiguration([]byte(renderBaseConfiguration), strategicPatch, jsonPatch)
	if err != nil {
		t.Fatalf("RenderEffectiveConfiguration() with patches error = %v", err)
	}
	if bytes.Equal(withoutPatches, withPatches) {
		t.Fatal("RenderEffectiveConfiguration() ignored configuration patches")
	}
	if bytes.Contains(withPatches, []byte("#")) {
		t.Fatal("RenderEffectiveConfiguration() retained YAML comments")
	}

	_, err = RenderEffectiveConfiguration([]byte(renderBaseConfiguration), []byte("machine: ["))
	if err == nil {
		t.Fatal("RenderEffectiveConfiguration() accepted malformed patch")
	}

	_, err = RenderEffectiveConfiguration([]byte(renderBaseConfiguration), nil)
	if !errors.Is(err, ErrConfigurationPatchEmpty) {
		t.Fatalf("RenderEffectiveConfiguration() empty patch error = %v, want ErrConfigurationPatchEmpty", err)
	}
}
