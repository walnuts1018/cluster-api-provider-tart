package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

const buildType = "https://cluster-api-provider-tart.walnuts.dev/build-types/mkosi/v1"

type statement struct {
	Type          string    `json:"_type"`
	Subject       []subject `json:"subject"`
	PredicateType string    `json:"predicateType"`
	Predicate     predicate `json:"predicate"`
}

type subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type predicate struct {
	BuildDefinition buildDefinition `json:"buildDefinition"`
	RunDetails      runDetails      `json:"runDetails"`
}

type buildDefinition struct {
	BuildType            string               `json:"buildType"`
	ExternalParameters   externalParameters   `json:"externalParameters"`
	ResolvedDependencies []resolvedDependency `json:"resolvedDependencies"`
}

type externalParameters struct {
	SourceURI      string `json:"sourceURI"`
	SourceRevision string `json:"sourceRevision"`
	Generation     uint64 `json:"artifactGeneration"`
}

type resolvedDependency struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

type runDetails struct {
	Builder builder `json:"builder"`
}

type builder struct {
	ID string `json:"id"`
}

func main() {
	var manifestPath string
	var lockPath string
	var sourceURI string
	var sourceRevision string
	var builderID string
	var outputPath string
	flag.StringVar(&manifestPath, "manifest", "", "Path to Artifact Manifest")
	flag.StringVar(&lockPath, "lock", "", "Path to build input lock file")
	flag.StringVar(&sourceURI, "source-uri", "", "Source repository URI")
	flag.StringVar(&sourceRevision, "source-revision", "", "Full source revision")
	flag.StringVar(&builderID, "builder-id", "", "Builder identity URI")
	flag.StringVar(&outputPath, "output", "", "Path to in-toto provenance output")
	flag.Parse()

	if err := run(manifestPath, lockPath, sourceURI, sourceRevision, builderID, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "provenance generation failed: %v\n", err)
		os.Exit(1)
	}
}

func run(manifestPath, lockPath, sourceURI, sourceRevision, builderID, outputPath string) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "manifest", value: manifestPath},
		{name: "lock", value: lockPath},
		{name: "source-uri", value: sourceURI},
		{name: "source-revision", value: sourceRevision},
		{name: "builder-id", value: builderID},
		{name: "output", value: outputPath},
	}
	for _, item := range required {
		if item.value == "" {
			return fmt.Errorf("-%s is required", item.name)
		}
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read Artifact Manifest: %w", err)
	}
	manifest, err := artifact.Parse(manifestData)
	if err != nil {
		return err
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("read build input lock: %w", err)
	}
	if len(lockData) == 0 {
		return errors.New("build input lock is empty")
	}

	provenance := create(manifest, lockPath, lockData, sourceURI, sourceRevision, builderID)
	encoded, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return fmt.Errorf("encode provenance: %w", err)
	}
	if err := os.WriteFile(outputPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write provenance: %w", err)
	}
	return nil
}

func create(
	manifest artifact.ValidatedManifest,
	lockPath string,
	lockData []byte,
	sourceURI string,
	sourceRevision string,
	builderID string,
) statement {
	value := manifest.Value()
	lockDigest := sha256.Sum256(lockData)
	return statement{
		Type: "https://in-toto.io/Statement/v1",
		Subject: []subject{
			{Name: "os.img", Digest: digestMap(value.Image.Digest)},
			{Name: "os.verity", Digest: digestMap(value.Verity.Digest)},
			{Name: "vmlinuz", Digest: digestMap(value.Boot.KernelDigest)},
			{Name: "initrd", Digest: digestMap(value.Boot.InitrdDigest)},
		},
		PredicateType: "https://slsa.dev/provenance/v1",
		Predicate: predicate{
			BuildDefinition: buildDefinition{
				BuildType: buildType,
				ExternalParameters: externalParameters{
					SourceURI:      sourceURI,
					SourceRevision: sourceRevision,
					Generation:     value.Generation,
				},
				ResolvedDependencies: []resolvedDependency{{
					URI:    lockPath,
					Digest: map[string]string{"sha256": hex.EncodeToString(lockDigest[:])},
				}},
			},
			RunDetails: runDetails{Builder: builder{ID: builderID}},
		},
	}
}

func digestMap(value string) map[string]string {
	return map[string]string{"sha256": value[len("sha256:"):]}
}
