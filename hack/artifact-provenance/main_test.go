package main

import (
	"strings"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

func TestCreateRecordsSubjectsAndBuildInputs(t *testing.T) {
	t.Parallel()

	manifest, err := artifact.Validate(artifact.Manifest{
		SchemaVersion: artifact.SchemaVersion,
		MediaType:     artifact.MediaType,
		OS:            artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:  "amd64",
		Filesystem:    "ext4",
		Image:         artifact.Payload{Digest: sha256Digest("a"), SizeBytes: 1},
		Verity: artifact.Verity{
			Digest: sha256Digest("b"), SizeBytes: 1, RootHash: strings.Repeat("c", 64),
		},
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", Version: "v1.35.0"},
		Boot:            artifact.Boot{KernelDigest: sha256Digest("d"), InitrdDigest: sha256Digest("e")},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      12,
		PlatformProfile: "amd64-uefi-ab/v1",
	})
	if err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}

	got := create(
		manifest,
		"artifact/locks/ubuntu.json",
		[]byte(`{"schemaVersion":1}`),
		"https://github.com/walnuts1018/cluster-api-provider-tart",
		strings.Repeat("f", 40),
		"https://github.com/actions/runner",
	)
	if got.PredicateType != "https://slsa.dev/provenance/v1" {
		t.Fatalf("PredicateType = %q", got.PredicateType)
	}
	if len(got.Subject) != 4 || got.Subject[0].Digest["sha256"] != strings.Repeat("a", 64) {
		t.Fatalf("Subject = %#v", got.Subject)
	}
	if got.Predicate.BuildDefinition.ExternalParameters.Generation != 12 {
		t.Fatalf(
			"artifactGeneration = %d",
			got.Predicate.BuildDefinition.ExternalParameters.Generation,
		)
	}
	if len(got.Predicate.BuildDefinition.ResolvedDependencies) != 1 {
		t.Fatalf(
			"len(ResolvedDependencies) = %d",
			len(got.Predicate.BuildDefinition.ResolvedDependencies),
		)
	}
}

func sha256Digest(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}
