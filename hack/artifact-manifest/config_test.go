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

package main

import (
	"path/filepath"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

func TestArtifactConfigは対応MatrixのManifestIdentityを満たす(t *testing.T) {
	t.Parallel()

	configs, err := filepath.Glob(filepath.Join("..", "..", "artifact", "config", "*.json"))
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v", err)
	}
	if len(configs) == 0 {
		t.Fatal("artifact config is empty")
	}
	for _, path := range configs {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := readConfig(path)
			if err != nil {
				t.Fatalf("readConfig() error = %v", err)
			}
			_, err = artifact.Validate(artifact.Manifest{
				SchemaVersion: artifact.SchemaVersion,
				MediaType:     artifact.MediaType,
				OS:            cfg.OS,
				Architecture:  cfg.Architecture,
				Filesystem:    cfg.Filesystem,
				Image: artifact.Payload{
					Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					SizeBytes: 1,
				},
				Verity: artifact.Verity{
					Digest:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					SizeBytes: 1,
					RootHash:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				},
				StateSchema: cfg.StateSchema,
				Kubernetes:  cfg.Kubernetes,
				Boot: artifact.Boot{
					KernelDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
					InitrdDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				},
				Requirements:    cfg.Requirements,
				Generation:      cfg.Generation,
				PlatformProfile: cfg.PlatformProfile,
			})
			if err != nil {
				t.Fatalf("artifact.Validate() error = %v", err)
			}
		})
	}
}
