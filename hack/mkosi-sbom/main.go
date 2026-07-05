package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
)

type mkosiManifest struct {
	ManifestVersion int            `json:"manifest_version"`
	Config          mkosiConfig    `json:"config"`
	Packages        []mkosiPackage `json:"packages"`
}

type mkosiConfig struct {
	Name         string `json:"name"`
	Distribution string `json:"distribution"`
	Architecture string `json:"architecture"`
	Release      string `json:"release"`
}

type mkosiPackage struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
}

type cyclonedxBOM struct {
	BOMFormat   string               `json:"bomFormat"`
	SpecVersion string               `json:"specVersion"`
	Version     int                  `json:"version"`
	Metadata    cyclonedxMetadata    `json:"metadata"`
	Components  []cyclonedxComponent `json:"components"`
}

type cyclonedxMetadata struct {
	Component cyclonedxComponent `json:"component"`
}

type cyclonedxComponent struct {
	Type       string              `json:"type"`
	BOMRef     string              `json:"bom-ref"`
	Name       string              `json:"name"`
	Version    string              `json:"version"`
	PURL       string              `json:"purl,omitempty"`
	Properties []cyclonedxProperty `json:"properties,omitempty"`
}

type cyclonedxProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func main() {
	var inputPath string
	var outputPath string
	flag.StringVar(&inputPath, "input", "", "Path to mkosi JSON manifest")
	flag.StringVar(&outputPath, "output", "", "Path to CycloneDX JSON output")
	flag.Parse()

	if err := run(inputPath, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "SBOM conversion failed: %v\n", err)
		os.Exit(1)
	}
}

func run(inputPath, outputPath string) error {
	if inputPath == "" {
		return errors.New("-input is required")
	}
	if outputPath == "" {
		return errors.New("-output is required")
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read mkosi manifest: %w", err)
	}
	var input mkosiManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return fmt.Errorf("decode mkosi manifest: %w", err)
	}
	output, err := convert(input)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CycloneDX SBOM: %w", err)
	}
	if err := os.WriteFile(outputPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write CycloneDX SBOM: %w", err)
	}
	return nil
}

func convert(input mkosiManifest) (cyclonedxBOM, error) {
	if input.ManifestVersion != 1 {
		return cyclonedxBOM{}, fmt.Errorf("unsupported mkosi manifest version: %d", input.ManifestVersion)
	}
	if input.Config.Name == "" || input.Config.Distribution == "" || input.Config.Architecture == "" || input.Config.Release == "" {
		return cyclonedxBOM{}, errors.New("mkosi image config is incomplete")
	}

	components := make([]cyclonedxComponent, 0, len(input.Packages))
	for _, item := range input.Packages {
		if item.Type != "deb" || item.Name == "" || item.Version == "" || item.Architecture == "" {
			return cyclonedxBOM{}, fmt.Errorf("invalid mkosi package: %q", item.Name)
		}
		purl := debianPURL(input.Config.Distribution, item)
		components = append(components, cyclonedxComponent{
			Type:    "library",
			BOMRef:  purl,
			Name:    item.Name,
			Version: item.Version,
			PURL:    purl,
			Properties: []cyclonedxProperty{
				{Name: "tart:package:type", Value: item.Type},
				{Name: "tart:package:architecture", Value: item.Architecture},
			},
		})
	}
	sort.Slice(components, func(left, right int) bool {
		return components[left].BOMRef < components[right].BOMRef
	})

	imageRef := fmt.Sprintf("pkg:generic/%s@%s?arch=%s",
		pathEscape(input.Config.Name),
		pathEscape(input.Config.Release),
		url.QueryEscape(input.Config.Architecture),
	)
	return cyclonedxBOM{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.6",
		Version:     1,
		Metadata: cyclonedxMetadata{Component: cyclonedxComponent{
			Type:    "operating-system",
			BOMRef:  imageRef,
			Name:    input.Config.Name,
			Version: input.Config.Release,
			PURL:    imageRef,
		}},
		Components: components,
	}, nil
}

func debianPURL(distribution string, item mkosiPackage) string {
	return fmt.Sprintf(
		"pkg:deb/%s/%s@%s?arch=%s",
		pathEscape(distribution),
		pathEscape(item.Name),
		pathEscape(item.Version),
		url.QueryEscape(item.Architecture),
	)
}

func pathEscape(value string) string {
	return url.PathEscape(value)
}
