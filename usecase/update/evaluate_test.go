package update_test

import (
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
	"github.com/walnuts1018/cluster-api-provider-tart/usecase/update"
)

type configurationOption func(*usecasebootstrap.MachineConfigurationContext)

func withMachineRole(role domainbootstrap.MachineRole) configurationOption {
	return func(input *usecasebootstrap.MachineConfigurationContext) { input.MachineRole = role }
}

func withEndpoint(endpoint string) configurationOption {
	return func(input *usecasebootstrap.MachineConfigurationContext) { input.ControlPlaneEndpoint = endpoint }
}

func withInstallDiskSerial(serial string) configurationOption {
	return func(input *usecasebootstrap.MachineConfigurationContext) { input.InstallDisk.Serial = serial }
}

func newSecretsBundle(t *testing.T) *secrets.Bundle {
	t.Helper()
	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	return bundle
}

func newConfiguration(t *testing.T, bundle *secrets.Bundle, options []configurationOption, patches ...[]byte) []byte {
	t.Helper()
	input := usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: "https://192.0.2.10:6443",
		KubernetesVersion:    "1.34.0",
		MachineRole:          domainbootstrap.MachineRoleWorker,
		SecretsBundle:        bundle,
		InstallDisk: &domainbootstrap.InstallDisk{
			DevicePath: "/dev/vda",
			SizeBytes:  64 * 1024 * 1024 * 1024,
			Serial:     "disk-a",
			Transport:  "virtio",
		},
	}
	for _, option := range options {
		option(&input)
	}
	configuration, err := configbuilder.GenerateMachineConfiguration(input, patches...)
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	configuration, err = talos.SetInstallerImage(configuration, "v1.14.0", "schematic")
	if err != nil {
		t.Fatalf("SetInstallerImage() error = %v", err)
	}
	return configuration
}

func TestEvaluateConfigurationChange(t *testing.T) {
	t.Parallel()

	bundle := newSecretsBundle(t)
	base := newConfiguration(t, bundle, nil)
	sysctl := newConfiguration(t, bundle, nil, []byte("machine:\n  sysctls:\n    net.core.somaxconn: \"1024\"\n"))
	userVolume := newConfiguration(t, bundle, nil, []byte("apiVersion: v1alpha1\nkind: UserVolumeConfig\nname: extra\nprovisioning:\n  diskSelector:\n    match: disk.transport == \"virtio\"\n  maxSize: 10GiB\n"))
	otherDisk := newConfiguration(t, bundle, []configurationOption{withInstallDiskSerial("disk-b")})
	controlPlane := newConfiguration(t, bundle, []configurationOption{withMachineRole(domainbootstrap.MachineRoleControlPlane)})
	otherEndpoint := newConfiguration(t, bundle, []configurationOption{withEndpoint("https://192.0.2.20:6443")})

	tests := map[string]struct {
		policy    bootstrapv1alpha1.ConfigurationApplyStrategy
		desired   []byte
		wantClass domainupdate.ChangeClass
		wantMode  domainupdate.ApplyMode
	}{
		"no difference": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyReboot,
			desired:   base,
			wantClass: domainupdate.ChangeNone,
		},
		"auto falls back to a controlled reboot": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyReboot,
			desired:   sysctl,
			wantClass: domainupdate.ChangeUpdatable,
			wantMode:  domainupdate.ApplyModeReboot,
		},
		"empty policy defaults to auto": {
			desired:   sysctl,
			wantClass: domainupdate.ChangeUpdatable,
			wantMode:  domainupdate.ApplyModeReboot,
		},
		"live policy applies without a reboot": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyNoReboot,
			desired:   sysctl,
			wantClass: domainupdate.ChangeUpdatable,
			wantMode:  domainupdate.ApplyModeNoReboot,
		},
		"reboot policy applies with a reboot": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyReboot,
			desired:   sysctl,
			wantClass: domainupdate.ChangeUpdatable,
			wantMode:  domainupdate.ApplyModeReboot,
		},
		"volume document is an ordinary Talos change": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyNoReboot,
			desired:   userVolume,
			wantClass: domainupdate.ChangeUpdatable,
			wantMode:  domainupdate.ApplyModeNoReboot,
		},
		"install target change is destructive": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyReboot,
			desired:   otherDisk,
			wantClass: domainupdate.ChangeReprovisionRequired,
		},
		"machine role change conflicts with a provider invariant": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyReboot,
			desired:   controlPlane,
			wantClass: domainupdate.ChangeInvariantConflict,
		},
		"control-plane endpoint change conflicts with a provider invariant": {
			policy:    bootstrapv1alpha1.ConfigurationApplyStrategyNoReboot,
			desired:   otherEndpoint,
			wantClass: domainupdate.ChangeInvariantConflict,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			decision, err := update.Evaluate(configbuilder.Builder{}, tt.policy, base, tt.desired)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if decision.Class != tt.wantClass {
				t.Fatalf("Evaluate() class = %q (%s), want %q", decision.Class, decision.Reason, tt.wantClass)
			}
			if decision.ApplyMode != tt.wantMode {
				t.Fatalf("Evaluate() apply mode = %q, want %q", decision.ApplyMode, tt.wantMode)
			}
			if tt.wantClass != domainupdate.ChangeNone && decision.Reason == "" {
				t.Fatal("Evaluate() returned no reason for a reported difference")
			}
		})
	}
}

// TestEvaluateIgnoresInstallerImageは、installer imageの差分がmachine configuration updateの判定へ混入しないことを確認する。
// installer imageの変更はTalos image upgrade pathが所有する。
func TestEvaluateIgnoresInstallerImage(t *testing.T) {
	t.Parallel()

	bundle := newSecretsBundle(t)
	base := newConfiguration(t, bundle, nil)
	upgraded, err := talos.SetInstallerImage(base, "v1.14.1", "schematic")
	if err != nil {
		t.Fatalf("SetInstallerImage() error = %v", err)
	}
	decision, err := update.Evaluate(configbuilder.Builder{}, bootstrapv1alpha1.ConfigurationApplyStrategyNoReboot, base, upgraded)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Class != domainupdate.ChangeNone {
		t.Fatalf("Evaluate() class = %q (%s), want %q", decision.Class, decision.Reason, domainupdate.ChangeNone)
	}
}

// TestEvaluateRejectsUnreadableConfigurationは、判定できないconfigurationをfail-closedで停止させることを確認する。
func TestEvaluateRejectsUnreadableConfiguration(t *testing.T) {
	t.Parallel()

	bundle := newSecretsBundle(t)
	base := newConfiguration(t, bundle, nil)
	if _, err := update.Evaluate(configbuilder.Builder{}, bootstrapv1alpha1.ConfigurationApplyStrategyNoReboot, base, []byte("not a machine configuration")); err == nil {
		t.Fatal("Evaluate() accepted an unreadable configuration")
	}
}
