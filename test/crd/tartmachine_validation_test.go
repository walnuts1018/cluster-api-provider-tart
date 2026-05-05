package crd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTartMachineCRDAllowsRealisticBootParameters(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tartmachines.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartMachine CRD: %v", err)
	}

	text := string(data)
	for _, rejectedPattern := range []string{
		"^[a-zA-Z0-9._-]+=[a-zA-Z0-9._-]+$",
		"^https?://[a-zA-Z0-9._-]+(?::\\d+)?(/[a-zA-Z0-9._-]+)*$|^/([a-zA-Z0-9._-]+)+$",
	} {
		if strings.Contains(text, rejectedPattern) {
			t.Fatalf("CRD still contains restrictive validation pattern %q", rejectedPattern)
		}
	}
}

func TestTartMachineCRDDefaultsBootstrapFormatToNoCloud(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tartmachines.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartMachine CRD: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"bootstrap:\n                default: {}",
		"format:\n                    default: NoCloud",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("CRD missing %q", want)
		}
	}
}

func TestTartMachineTemplateCRDDefaultsBootstrapFormatToNoCloud(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartMachineTemplate CRD: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"bootstrap:\n                        default: {}",
		"format:\n                            default: NoCloud",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("CRD missing %q", want)
		}
	}
}
