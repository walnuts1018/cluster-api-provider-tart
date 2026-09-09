package bootstrap

import (
	"bytes"
	"cmp"
	"errors"
	"slices"
)

// Layerはmachine configuration合成pipelineにおける段階を表す。値が小さいlayerほど先に適用され、
// 後続のlayerが先行layerを上書きできる。
type Layer int

const (
	// LayerBaseはTalos machineryが生成したprovider-owned base configurationである。
	LayerBase Layer = iota
	// LayerUserRawPatchはユーザーがimmutable Secretとして提供したraw patchである。
	LayerUserRawPatch
	// LayerProviderInvariantはinstall diskやProviderIDなど、providerが最後に強制するinvariantである。
	// ユーザーpatchで上書きできないよう、常に最後に適用する。
	LayerProviderInvariant
)

// ErrConfigPatchOrderInvalidは、patchの並びがbase→user raw patch→provider invariantの順序を満たさないことを示す。
var ErrConfigPatchOrderInvalid = errors.New("machine configuration patch order is invalid")

// ConfigPatchは、合成待ちのraw machine configuration patchの内容とlayerを表す値オブジェクトである。
// Contentの解釈(YAML/JSON patchの適用)はadapter/talos/configbuilderへ委譲する。
type ConfigPatch struct {
	Layer   Layer
	Content []byte
}

// OrderPatchesは、patchをlayerの昇順(base→user raw patch→provider invariant)へ安定ソートし、
// 各patchが空でなく既知のlayerに属することを検証する。同一layer内では入力順序を保持する。
// 実際のmerge処理は行わず、どの順序で適用すべきかという意思決定・検証だけを行う。
func OrderPatches(patches []ConfigPatch) ([]ConfigPatch, error) {
	ordered := make([]ConfigPatch, len(patches))
	copy(ordered, patches)

	for _, patch := range ordered {
		if len(bytes.TrimSpace(patch.Content)) == 0 {
			return nil, ErrConfigurationPatchEmpty
		}
		if patch.Layer < LayerBase || patch.Layer > LayerProviderInvariant {
			return nil, ErrConfigPatchOrderInvalid
		}
	}

	slices.SortStableFunc(ordered, func(a, b ConfigPatch) int {
		return cmp.Compare(a.Layer, b.Layer)
	})

	return ordered, nil
}
