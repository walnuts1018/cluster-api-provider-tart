package configbuilder

import (
	"errors"
	"testing"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

func TestDigestEffectiveConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		first      string
		second     string
		sameDigest bool
		wantErr    error
	}{
		"normalizes YAML representation": {
			first:      "version: v1alpha1\nmachine:\n  type: worker\n  token: token-a\n",
			second:     "# equivalent input\nmachine:\n  token: token-a\n  type: worker\nversion: v1alpha1\n",
			sameDigest: true,
		},
		"redacts machine token": {
			first:      "version: v1alpha1\nmachine:\n  type: worker\n  token: token-a\n",
			second:     "version: v1alpha1\nmachine:\n  type: worker\n  token: token-b\n",
			sameDigest: true,
		},
		"retains non-secret changes": {
			first:  "version: v1alpha1\nmachine:\n  type: worker\n  token: token-a\n",
			second: "version: v1alpha1\nmachine:\n  type: controlplane\n  token: token-a\n",
		},
		"rejects empty configuration": {
			wantErr: domainbootstrap.ErrCompleteConfigurationEmpty,
		},
		"rejects malformed configuration": {
			first:   "version: [",
			wantErr: domainbootstrap.ErrEffectiveConfigurationInvalid,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			first, err := DigestEffectiveConfiguration([]byte(tt.first))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("DigestEffectiveConfiguration() error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("DigestEffectiveConfiguration(first) error = %v", err)
			}

			second, err := DigestEffectiveConfiguration([]byte(tt.second))
			if err != nil {
				t.Fatalf("DigestEffectiveConfiguration(second) error = %v", err)
			}
			if (first == second) != tt.sameDigest {
				t.Fatalf("digest equality = %t, want %t", first == second, tt.sameDigest)
			}
		})
	}
}
