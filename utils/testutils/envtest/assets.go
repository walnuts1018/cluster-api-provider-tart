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

package envtestutil

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	clusterAPIFixtureDir = "test/envtest/crds/cluster-api/v1.13.1"
	missingAssetsMessage = "envtest binaries are required; run `mise run setup-envtest` or set KUBEBUILDER_ASSETS"
)

func BinaryAssetsDirectory(projectRoot string) string {
	if assets := os.Getenv("KUBEBUILDER_ASSETS"); assets != "" {
		if stat, err := os.Stat(assets); err == nil && stat.IsDir() {
			return assets
		}
	}

	basePath := filepath.Join(projectRoot, "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

func RequireBinaryAssetsDirectory(tb testing.TB, projectRoot string) string {
	tb.Helper()
	assets := BinaryAssetsDirectory(projectRoot)
	if assets == "" {
		tb.Fatal(missingAssetsMessage)
	}
	return assets
}

func ClusterAPICRDDirectory(projectRoot string) string {
	return filepath.Join(projectRoot, clusterAPIFixtureDir)
}

func MissingAssetsMessage() string {
	return missingAssetsMessage
}
