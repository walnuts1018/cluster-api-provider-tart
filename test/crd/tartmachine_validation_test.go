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

func TestTartMachineCRDUsesAgentOnlyProvisioningSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tartmachines.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartMachine CRD: %v", err)
	}

	text := string(data)
	for _, rejected := range []string{
		"- Preseed",
		"- Raw",
		"initrd:",
	} {
		if strings.Contains(text, rejected) {
			t.Fatalf("CRD still contains legacy field or bootstrap format %q", rejected)
		}
	}
	for _, want := range []string{
		"- image",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("CRD missing agent provisioning schema %q", want)
		}
	}
}

func TestTartHostCRDRequiresProvisioningDevice(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tarthosts.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartHost CRD: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"- provisioning",
		"- device",
		"/dev/(sd[a-z]+|vd[a-z]+|xvd[a-z]+|nvme[0-9]+n[0-9]+|disk/by-id/",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("CRD missing provisioning device schema %q", want)
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
