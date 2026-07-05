// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package artifact

import (
	"strings"
	"testing"
)

func TestValidateManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr string
	}{
		{name: "valid", mutate: func(*Manifest) {}},
		{
			name: "image digest",
			mutate: func(manifest *Manifest) {
				manifest.Image.Digest = "sha256:INVALID"
			},
			wantErr: "image.digest",
		},
		{
			name: "image size",
			mutate: func(manifest *Manifest) {
				manifest.Image.SizeBytes = 0
			},
			wantErr: "image.sizeBytes",
		},
		{
			name: "verity root hash",
			mutate: func(manifest *Manifest) {
				manifest.Verity.RootHash = strings.Repeat("A", 64)
			},
			wantErr: "verity.rootHash",
		},
		{
			name: "state schema range",
			mutate: func(manifest *Manifest) {
				manifest.StateSchema = StateSchema{Min: 2, Max: 1}
			},
			wantErr: "stateSchema.max",
		},
		{
			name: "generation",
			mutate: func(manifest *Manifest) {
				manifest.Generation = 0
			},
			wantErr: "generation",
		},
		{
			name: "platform profile version",
			mutate: func(manifest *Manifest) {
				manifest.PlatformProfile = "amd64-uefi-ab"
			},
			wantErr: "platformProfile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manifest := validManifest()
			tt.mutate(&manifest)

			_, err := Validate(manifest)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseRejectsUnknownAndTrailingData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{
			name: "unknown field",
			data: `{"schemaVersion":1,"unknown":true}`,
		},
		{
			name: "trailing JSON",
			data: `{"schemaVersion":1}{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(tt.data)); err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
		})
	}
}

func TestCanonicalJSONIsStable(t *testing.T) {
	t.Parallel()

	validated, err := Validate(validManifest())
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	first, err := validated.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() first error = %v", err)
	}
	second, err := validated.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() second error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("CanonicalJSON() differs:\nfirst:  %s\nsecond: %s", first, second)
	}
	if !strings.HasPrefix(string(first), `{"architecture":"amd64","boot":`) {
		t.Fatalf("CanonicalJSON() = %s, want RFC 8785 key order", first)
	}

	manifestDigest, err := validated.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if got := manifestDigest.String(); len(got) != len("sha256:")+64 {
		t.Fatalf("Digest() = %q, want sha256 digest", got)
	}
}

func validManifest() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		MediaType:     MediaType,
		OS: OS{
			Family:  "ubuntu",
			Version: "24.04",
		},
		Architecture: "amd64",
		Filesystem:   "ext4",
		Image: Payload{
			Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SizeBytes: 8589934592,
		},
		Verity: Verity{
			Digest:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			SizeBytes: 536870912,
			RootHash:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		},
		StateSchema: StateSchema{Min: 1, Max: 1},
		Kubernetes: Kubernetes{
			Distribution: "kubeadm",
			Version:      "v1.35.0",
		},
		Boot: Boot{
			KernelDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			InitrdDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		},
		Requirements:    Requirements{CPULevel: "x86-64-v1"},
		Generation:      1,
		PlatformProfile: "amd64-uefi-ab/v1",
	}
}
