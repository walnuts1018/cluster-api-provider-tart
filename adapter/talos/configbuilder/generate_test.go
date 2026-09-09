package configbuilder

import (
	"bytes"
	"errors"
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
)

func TestCanonicalEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "adds HTTPS scheme", input: "192.0.2.10:6443", want: "https://192.0.2.10:6443"},
		{name: "removes root path", input: "https://[2001:db8::10]:6443/", want: "https://[2001:db8::10]:6443"},
		{name: "rejects path", input: "https://192.0.2.10:6443/api", wantErr: true},
		{name: "rejects query", input: "https://192.0.2.10:6443?next=admin", wantErr: true},
		{name: "rejects fragment", input: "https://192.0.2.10:6443#api", wantErr: true},
		{name: "rejects userinfo", input: "https://operator@192.0.2.10:6443", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := canonicalEndpoint(tt.input)
			if tt.wantErr {
				if !errors.Is(err, domainbootstrap.ErrMachineConfigurationContextIncomplete) {
					t.Fatalf("canonicalEndpoint() error = %v, want incomplete context error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("canonicalEndpoint() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("canonicalEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateMachineConfigurationRejectsIncompleteContext(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	base := usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: "192.0.2.10:6443",
		KubernetesVersion:    "v1.34.0",
		MachineRole:          domainbootstrap.MachineRoleWorker,
		SecretsBundle:        bundle,
	}

	tests := []struct {
		name string
		edit func(*usecasebootstrap.MachineConfigurationContext)
	}{
		{name: "cluster name", edit: func(input *usecasebootstrap.MachineConfigurationContext) { input.ClusterName = "" }},
		{name: "control-plane endpoint", edit: func(input *usecasebootstrap.MachineConfigurationContext) {
			input.ControlPlaneEndpoint = "not an endpoint"
		}},
		{name: "Kubernetes version", edit: func(input *usecasebootstrap.MachineConfigurationContext) { input.KubernetesVersion = "" }},
		{name: "machine role", edit: func(input *usecasebootstrap.MachineConfigurationContext) {
			input.MachineRole = domainbootstrap.MachineRole(99)
		}},
		{name: "secrets bundle", edit: func(input *usecasebootstrap.MachineConfigurationContext) { input.SecretsBundle = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := base
			tt.edit(&input)
			if _, err := GenerateMachineConfiguration(input); !errors.Is(err, domainbootstrap.ErrMachineConfigurationContextIncomplete) {
				t.Fatalf("GenerateMachineConfiguration() error = %v, want incomplete context error", err)
			}
		})
	}
}

func TestGenerateMachineConfigurationNormalizesEndpointAndVersion(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	configuration, err := GenerateMachineConfiguration(usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: " 192.0.2.10:6443 ",
		KubernetesVersion:    "v1.34.0",
		MachineRole:          domainbootstrap.MachineRoleControlPlane,
		SecretsBundle:        bundle,
	})
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		t.Fatalf("configloader.NewFromBytes() error = %v", err)
	}
	if got := provider.K8sClusterConfig().ClusterEndpoint().String(); got != "https://192.0.2.10:6443" {
		t.Errorf("cluster endpoint = %q, want canonical HTTPS endpoint", got)
	}
	if got := provider.Machine().Type().String(); got != "controlplane" {
		t.Errorf("machine type = %q, want controlplane", got)
	}
}

// TestGenerateMachineConfigurationRawPatchCanDisableSchedulingTaintAndDefaultCNIは、control planeの
// NoSchedule taint撤廃とdefault CNI(Flannel)無効化を、専用のCRD field/generate optionなしに、
// user raw patchのTalos `$patch: delete`構文だけで実現できることを検証する回帰テストである。
func TestGenerateMachineConfigurationRawPatchCanDisableSchedulingTaintAndDefaultCNI(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	base := usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: "192.0.2.10:6443",
		KubernetesVersion:    "v1.34.0",
		MachineRole:          domainbootstrap.MachineRoleControlPlane,
		SecretsBundle:        bundle,
	}

	defaultConfiguration, err := GenerateMachineConfiguration(base)
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	if !bytes.Contains(defaultConfiguration, []byte("KubeFlannelCNIConfig")) {
		t.Error("default configuration should include KubeFlannelCNIConfig")
	}
	if !bytes.Contains(defaultConfiguration, []byte("NoSchedule")) {
		t.Error("default configuration should taint control-plane nodes with NoSchedule")
	}

	patch := []byte(`apiVersion: v1alpha1
kind: KubeNodeConfig
taints:
  $patch: delete
---
apiVersion: v1alpha1
kind: KubeFlannelCNIConfig
$patch: delete
`)
	configuration, err := GenerateMachineConfiguration(base, patch)
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	if bytes.Contains(configuration, []byte("KubeFlannelCNIConfig")) {
		t.Error("a raw patch deleting the KubeFlannelCNIConfig document should remove it from the generated configuration")
	}
	if bytes.Contains(configuration, []byte("NoSchedule")) {
		t.Error("a raw patch deleting KubeNodeConfig.taints should omit the control-plane NoSchedule taint")
	}
	if _, err := configloader.NewFromBytes(configuration); err != nil {
		t.Fatalf("configloader.NewFromBytes() error = %v", err)
	}
}

func TestGenerateMachineConfigurationStaticHostname(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	input := usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: "192.0.2.10:6443",
		KubernetesVersion:    "v1.34.0",
		MachineRole:          domainbootstrap.MachineRoleControlPlane,
		SecretsBundle:        bundle,
		Hostname:             "eclair",
	}

	configuration, err := GenerateMachineConfiguration(input)
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	if !bytes.Contains(configuration, []byte("hostname: eclair")) {
		t.Error("configuration should set the static hostname")
	}
	if bytes.Contains(configuration, []byte("auto: stable")) {
		t.Error("configuration should not keep the default auto-generated hostname alongside a static one")
	}
	if _, err := configloader.NewFromBytes(configuration); err != nil {
		t.Fatalf("configloader.NewFromBytes() error = %v", err)
	}
}
