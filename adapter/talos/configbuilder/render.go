package configbuilder

import (
	"bytes"
	"fmt"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

// RenderEffectiveConfigurationはcompleteなbase configurationへraw patchを順番に適用し、Talosが解釈するcanonical YAMLを返す。
// patchの解釈とmergeはTalos machineryへ委譲し、独自のYAML merge semanticsは持たない。
func RenderEffectiveConfiguration(base []byte, patches ...[]byte) ([]byte, error) {
	if len(bytes.TrimSpace(base)) == 0 {
		return nil, domainbootstrap.ErrCompleteConfigurationEmpty
	}

	domainPatches := make([]domainbootstrap.ConfigPatch, len(patches))
	for index, patchBytes := range patches {
		domainPatches[index] = domainbootstrap.ConfigPatch{Layer: domainbootstrap.LayerUserRawPatch, Content: patchBytes}
	}
	// 全patchが同一layer(ユーザーraw patch)であるため、domain.OrderPatchesは非空性の検証だけを行い、
	// 呼び出し側が渡した順序を維持する。実際のpatch解釈・適用はTalos machineryへ委譲する。
	ordered, err := domainbootstrap.OrderPatches(domainPatches)
	if err != nil {
		return nil, err
	}

	input := configpatcher.WithBytes(bytes.Clone(base))
	for index, patch := range ordered {
		loaded, err := configpatcher.LoadPatch(patch.Content)
		if err != nil {
			return nil, fmt.Errorf("load machine configuration patch %d: %w", index, err)
		}
		input, err = configpatcher.Apply(input, []configpatcher.Patch{loaded})
		if err != nil {
			return nil, fmt.Errorf("apply machine configuration patch %d: %w", index, err)
		}
	}

	marshaled, err := input.Bytes()
	if err != nil {
		return nil, fmt.Errorf("marshal effective machine configuration: %w", err)
	}

	provider, err := configloader.NewFromBytes(marshaled)
	if err != nil {
		return nil, fmt.Errorf("load rendered machine configuration: %w", err)
	}
	if !provider.CompleteForBoot() {
		return nil, domainbootstrap.ErrEffectiveConfigurationIncomplete
	}

	canonical, err := provider.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return nil, fmt.Errorf("encode effective machine configuration: %w", err)
	}

	return canonical, nil
}
