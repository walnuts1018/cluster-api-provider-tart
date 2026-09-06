package bootstrap

import (
	"errors"
	"testing"
)

func TestOrderPatches(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		patches []ConfigPatch
		want    []Layer
		wantErr error
	}{
		"reorders base, user patch and provider invariant": {
			patches: []ConfigPatch{
				{Layer: LayerProviderInvariant, Content: []byte("invariant")},
				{Layer: LayerBase, Content: []byte("base")},
				{Layer: LayerUserRawPatch, Content: []byte("patch")},
			},
			want: []Layer{LayerBase, LayerUserRawPatch, LayerProviderInvariant},
		},
		"preserves input order within the same layer": {
			patches: []ConfigPatch{
				{Layer: LayerUserRawPatch, Content: []byte("first")},
				{Layer: LayerUserRawPatch, Content: []byte("second")},
			},
			want: []Layer{LayerUserRawPatch, LayerUserRawPatch},
		},
		"rejects empty patch content": {
			patches: []ConfigPatch{{Layer: LayerBase, Content: nil}},
			wantErr: ErrConfigurationPatchEmpty,
		},
		"rejects unknown layer": {
			patches: []ConfigPatch{{Layer: Layer(99), Content: []byte("x")}},
			wantErr: ErrConfigPatchOrderInvalid,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := OrderPatches(tt.patches)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("OrderPatches() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("OrderPatches() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("OrderPatches() length = %d, want %d", len(got), len(tt.want))
			}
			for i, layer := range tt.want {
				if got[i].Layer != layer {
					t.Fatalf("OrderPatches()[%d].Layer = %v, want %v", i, got[i].Layer, layer)
				}
			}
		})
	}
}
