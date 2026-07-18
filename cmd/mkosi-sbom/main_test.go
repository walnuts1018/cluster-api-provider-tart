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
	"os"
	"path/filepath"
	"testing"
)

func TestConvertIncludesSortedDebPackages(t *testing.T) {
	t.Parallel()

	bom, err := convert(mkosiManifest{
		ManifestVersion: 1,
		Config: mkosiConfig{
			Name:         "tart-ubuntu",
			Distribution: "ubuntu",
			Architecture: "x86-64",
			Release:      "noble",
		},
		Packages: []mkosiPackage{
			{Type: "deb", Name: "zlib1g", Version: "1:1.3.dfsg-3.1ubuntu2", Architecture: "amd64"},
			{Type: "deb", Name: "base-files", Version: "13ubuntu10.3", Architecture: "amd64"},
		},
	})
	if err != nil {
		t.Fatalf("convert() error = %v", err)
	}
	if bom.BOMFormat != "CycloneDX" || bom.SpecVersion != "1.6" {
		t.Fatalf("convert() format = %s %s", bom.BOMFormat, bom.SpecVersion)
	}
	if len(bom.Components) != 2 {
		t.Fatalf("len(Components) = %d, want 2", len(bom.Components))
	}
	if bom.Components[0].Name != "base-files" || bom.Components[1].Name != "zlib1g" {
		t.Fatalf("components are not sorted: %#v", bom.Components)
	}
	if bom.Components[0].PURL != "pkg:deb/ubuntu/base-files@13ubuntu10.3?arch=amd64" {
		t.Fatalf("base-files PURL = %q", bom.Components[0].PURL)
	}
}

func TestConvertRejectsUnsupportedPackageType(t *testing.T) {
	t.Parallel()

	_, err := convert(mkosiManifest{
		ManifestVersion: 1,
		Config: mkosiConfig{
			Name:         "tart-ubuntu",
			Distribution: "ubuntu",
			Architecture: "x86-64",
			Release:      "noble",
		},
		Packages: []mkosiPackage{
			{Type: "rpm", Name: "systemd", Version: "260", Architecture: "x86_64"},
		},
	})
	if err == nil {
		t.Fatal("convert() error = nil, want unsupported package error")
	}
}

func TestRunIgnoresUnsupportedMkosiManifestFields(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	inputPath := filepath.Join(directory, "manifest.json")
	outputPath := filepath.Join(directory, "sbom.json")
	manifest := `{
  "manifest_version": 1,
  "output_format": "disk",
  "config": {
    "name": "tart-ubuntu",
    "distribution": "ubuntu",
    "architecture": "x86-64",
    "release": "noble"
  },
  "packages": [
    {
      "type": "deb",
      "name": "base-files",
      "version": "13ubuntu10.3",
      "architecture": "amd64",
      "source": "mkosi"
    }
  ]
}`
	if err := os.WriteFile(inputPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := run(inputPath, outputPath); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("stat output: %v", err)
	}
}
