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
	"strings"
	"testing"
)

func TestRunAcceptsConsistentReleaseDocs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	initialProvisioningEvidence := writeEvidenceFile(t, root, "07-initial-provisioning-simulated-record.md")
	lifecycleEvidence := writeEvidenceFile(t, root, "09-kubernetes-lifecycle-simulated-record.md")
	matrixPath := filepath.Join(root, "release-matrix.yaml")
	releaseNotePath := filepath.Join(root, "unreleased.md")
	writeTestDoc(t, matrixPath, `
entries:
  - name: InitialProvisioning Ubuntu 24.04 amd64 UEFI kubeadm
    category: InitialProvisioning
    status: Supported
    evidencePaths:
      - `+initialProvisioningEvidence+`
    notes:
      - Release gate satisfied
  - name: KubernetesBinary single control plane
    category: KubernetesBinaryUpdate
    status: Experimental
    evidencePaths:
      - `+lifecycleEvidence+`
      - https://github.com/example/actions/runs/11
    notes:
      - Feature gate required
`)
	writeTestDoc(t, releaseNotePath, `
# Unreleased

## Experimental

- KubernetesBinary single control plane remains Experimental and requires a feature gate.

## Known Constraints

- single control plane KubernetesBinary update remains Experimental and requires a feature gate.
- management API outage coverage is still incomplete, so this path is not promoted to Supported.
- StateMigration automatic recovery is unavailable. RecoveryRequired needs manual recovery.
`)

	if err := run(options{
		matrixPath:      matrixPath,
		releaseNotePath: releaseNotePath,
	}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunRejectsMissingExperimentalEntryInReleaseNote(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lifecycleEvidence := writeEvidenceFile(t, root, "09-kubernetes-lifecycle-simulated-record.md")
	matrixPath := filepath.Join(root, "release-matrix.yaml")
	releaseNotePath := filepath.Join(root, "unreleased.md")
	writeTestDoc(t, matrixPath, `
- name: KubernetesBinary single control plane
  category: KubernetesBinaryUpdate
  status: Experimental
  evidencePaths:
    - `+lifecycleEvidence+`
  notes:
    - Feature gate required
`)
	writeTestDoc(t, releaseNotePath, `
# Unreleased

## Experimental

- OSOnly worker remains Experimental and requires a feature gate.

## Known Constraints

- single control plane KubernetesBinary update remains Experimental and requires a feature gate.
- management API outage coverage is still incomplete, so this path is not promoted to Supported.
- StateMigration automatic recovery is unavailable. RecoveryRequired needs manual recovery.
`)

	err := run(options{
		matrixPath:      matrixPath,
		releaseNotePath: releaseNotePath,
	})
	if err == nil {
		t.Fatal("run() error = nil, want mismatch error")
	}
	if !strings.Contains(err.Error(), "experimental matrix entry") {
		t.Fatalf("run() error = %v, want experimental matrix entry error", err)
	}
}

func TestRunRejectsMissingKnownConstraint(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lifecycleEvidence := writeEvidenceFile(t, root, "09-kubernetes-lifecycle-simulated-record.md")
	matrixPath := filepath.Join(root, "release-matrix.yaml")
	releaseNotePath := filepath.Join(root, "unreleased.md")
	writeTestDoc(t, matrixPath, `
- name: KubernetesBinary single control plane
  category: KubernetesBinaryUpdate
  status: Experimental
  evidencePaths:
    - `+lifecycleEvidence+`
  notes:
    - Feature gate required
`)
	writeTestDoc(t, releaseNotePath, `
# Unreleased

## Experimental

- KubernetesBinary single control plane remains Experimental and requires a feature gate.

## Known Constraints

- single control plane KubernetesBinary update remains Experimental and requires a feature gate.
- StateMigration automatic recovery is unavailable. RecoveryRequired needs manual recovery.
`)

	err := run(options{
		matrixPath:      matrixPath,
		releaseNotePath: releaseNotePath,
	})
	if err == nil {
		t.Fatal("run() error = nil, want missing known constraint error")
	}
	if !strings.Contains(err.Error(), "management API outage") {
		t.Fatalf("run() error = %v, want management API outage error", err)
	}
}

func TestRunRejectsMissingRequiredMatrixFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	matrixPath := filepath.Join(root, "release-matrix.yaml")
	releaseNotePath := filepath.Join(root, "unreleased.md")
	writeTestDoc(t, matrixPath, `
- name: ""
  category: ""
  status: Preview
  evidencePaths: []
  notes: []
`)
	writeTestDoc(t, releaseNotePath, `
# Unreleased

## Experimental

- none

## Known Constraints

- single control plane KubernetesBinary update remains Experimental and requires a feature gate.
- management API outage coverage is still incomplete, so this path is not promoted to Supported.
- StateMigration automatic recovery is unavailable. RecoveryRequired needs manual recovery.
`)

	err := run(options{
		matrixPath:      matrixPath,
		releaseNotePath: releaseNotePath,
	})
	if err == nil {
		t.Fatal("run() error = nil, want matrix validation error")
	}
	for _, fragment := range []string{"name is required", "status must be", "evidencePaths must contain at least one path"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("run() error = %v, want %q", err, fragment)
		}
	}
}

func writeTestDoc(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}

func writeEvidenceFile(t *testing.T, root, name string) string {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("evidence\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
	return path
}
