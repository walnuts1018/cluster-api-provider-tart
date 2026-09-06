package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestComputeDigest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input   []byte
		wantErr error
	}{
		"rejects empty input": {
			input:   nil,
			wantErr: ErrCanonicalConfigurationEmpty,
		},
		"rejects whitespace-only input": {
			input:   []byte("   \n"),
			wantErr: ErrCanonicalConfigurationEmpty,
		},
		"hashes canonical bytes": {
			input: []byte("version: v1alpha1\n"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := ComputeDigest(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ComputeDigest() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ComputeDigest() error = %v", err)
			}
			sum := sha256.Sum256(tt.input)
			if want := hex.EncodeToString(sum[:]); got != want {
				t.Fatalf("ComputeDigest() = %q, want %q", got, want)
			}
		})
	}
}
