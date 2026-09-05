package bootstrap

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
)

var ErrConfigurationPatchEmpty = errors.New("machine configuration patch is empty")

// RenderEffectiveConfigurationはcompleteなbase configurationへraw patchを順番に適用し、Talosが解釈するcanonical YAMLを返す。
// patchの解釈とmergeはTalos machineryへ委譲し、独自のYAML merge semanticsは持たない。
func RenderEffectiveConfiguration(base []byte, patches ...[]byte) ([]byte, error) {
	if len(bytes.TrimSpace(base)) == 0 {
		return nil, ErrCompleteConfigurationEmpty
	}

	input := configpatcher.WithBytes(bytes.Clone(base))
	for index, patchBytes := range patches {
		if len(bytes.TrimSpace(patchBytes)) == 0 {
			return nil, fmt.Errorf("%w: patch %d", ErrConfigurationPatchEmpty, index)
		}

		patch, err := configpatcher.LoadPatch(patchBytes)
		if err != nil {
			return nil, fmt.Errorf("load machine configuration patch %d: %w", index, err)
		}
		input, err = configpatcher.Apply(input, []configpatcher.Patch{patch})
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
		return nil, ErrEffectiveConfigurationIncomplete
	}

	canonical, err := provider.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return nil, fmt.Errorf("encode effective machine configuration: %w", err)
	}

	return canonical, nil
}
