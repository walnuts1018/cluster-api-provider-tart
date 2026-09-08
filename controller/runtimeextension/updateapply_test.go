package runtimeextension

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
)

// fakeUpdateNodeはTalos APIを必要とせずmachine configuration updateの判断を検証するための実装である。
type fakeUpdateNode struct {
	active        []byte
	activeErr     error
	liveErr       error
	rebootErr     error
	servicesErr   error
	bootTime      uint64
	liveApplies   int
	rebootApplies int
}

func (f *fakeUpdateNode) ActiveMachineConfiguration(context.Context) ([]byte, error) {
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	return f.active, nil
}

func (f *fakeUpdateNode) ApplyConfigurationNoReboot(_ context.Context, configuration []byte) error {
	f.liveApplies++
	if f.liveErr != nil {
		return f.liveErr
	}
	f.active = configuration
	return nil
}

func (f *fakeUpdateNode) ApplyConfiguration(_ context.Context, configuration []byte) error {
	f.rebootApplies++
	if f.rebootErr != nil {
		return f.rebootErr
	}
	f.active = configuration
	return nil
}

func (f *fakeUpdateNode) Reboot(context.Context) error {
	if f.rebootErr != nil {
		return f.rebootErr
	}
	f.bootTime++
	return nil
}

func (f *fakeUpdateNode) BootTime(context.Context) (uint64, error) {
	return f.bootTime, nil
}

func (f *fakeUpdateNode) ServicesHealthy(context.Context) error {
	return f.servicesErr
}

func newUpdateConfiguration(t *testing.T, bundle *secrets.Bundle, serial string, patches ...[]byte) []byte {
	t.Helper()
	configuration, err := configbuilder.GenerateMachineConfiguration(usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: "https://192.0.2.10:6443",
		KubernetesVersion:    "1.34.0",
		MachineRole:          domainbootstrap.MachineRoleWorker,
		SecretsBundle:        bundle,
		InstallDisk: &domainbootstrap.DiskIdentity{
			DevicePath: "/dev/vda",
			SizeBytes:  64 * 1024 * 1024 * 1024,
			Serial:     serial,
			Transport:  "virtio",
		},
	}, patches...)
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	configuration, err = talos.SetInstallerImage(configuration, "v1.14.0", "schematic")
	if err != nil {
		t.Fatalf("SetInstallerImage() error = %v", err)
	}
	return configuration
}

func TestApplyConfigurationUpdate(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	active := newUpdateConfiguration(t, bundle, "disk-a")
	safeDifference := newUpdateConfiguration(t, bundle, "disk-a", []byte("machine:\n  sysctls:\n    net.core.somaxconn: \"1024\"\n"))
	destructiveDifference := newUpdateConfiguration(t, bundle, "disk-b")

	t.Run("live policy applies without a reboot", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		outcome := ApplyConfigurationUpdate(t.Context(), configurationUpdate{
			node:     node,
			strategy: bootstrapv1alpha1.ConfigurationApplyStrategyApplyOnly,
			desired:  safeDifference,
		})
		if outcome.RetryMessage == "" || outcome.FailureMessage != "" {
			t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, want a retry", outcome)
		}
		if node.liveApplies != 1 || node.rebootApplies != 0 {
			t.Fatalf("ApplyConfigurationUpdate() applied live = %d, reboot = %d", node.liveApplies, node.rebootApplies)
		}
	})

	t.Run("live policy does not fall back to a reboot", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100, liveErr: errors.New("talos rejected the live apply")}
		outcome := ApplyConfigurationUpdate(t.Context(), configurationUpdate{
			node:     node,
			strategy: bootstrapv1alpha1.ConfigurationApplyStrategyApplyOnly,
			desired:  safeDifference,
		})
		if outcome.FailureMessage == "" || outcome.Done {
			t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, want a failure", outcome)
		}
		if node.rebootApplies != 0 {
			t.Fatalf("ApplyConfigurationUpdate() fell back to %d reboot applies", node.rebootApplies)
		}
	})

	t.Run("reboot policy waits for the drain gate", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		gateCalls := 0
		outcome := ApplyConfigurationUpdate(t.Context(), configurationUpdate{
			node:     node,
			strategy: bootstrapv1alpha1.ConfigurationApplyStrategyStagedReboot,
			desired:  safeDifference,
			rebootGate: func(context.Context) (bool, string) {
				gateCalls++
				return false, "The Node drain was blocked."
			},
		})
		if gateCalls != 1 || outcome.RetryMessage != "The Node drain was blocked." {
			t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, gate calls = %d", outcome, gateCalls)
		}
		if node.rebootApplies != 0 || node.liveApplies != 0 {
			t.Fatal("ApplyConfigurationUpdate() applied a configuration while the drain gate was closed")
		}
	})

	t.Run("reboot policy applies after the drain gate", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		outcome := ApplyConfigurationUpdate(t.Context(), configurationUpdate{
			node:                      node,
			strategy:                  bootstrapv1alpha1.ConfigurationApplyStrategyStagedReboot,
			desired:                   safeDifference,
			rebootGate:                func(context.Context) (bool, string) { return true, "" },
			rebootObservationTimeout:  time.Millisecond,
			rebootObservationInterval: time.Millisecond,
		})
		if outcome.RetryMessage == "" || outcome.FailureMessage != "" {
			t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, want a retry", outcome)
		}
		if node.rebootApplies != 1 || node.liveApplies != 0 {
			t.Fatalf("ApplyConfigurationUpdate() applied live = %d, reboot = %d", node.liveApplies, node.rebootApplies)
		}
		if node.bootTime != 101 {
			t.Fatalf("ApplyConfigurationUpdate() did not observe a reboot, boot time = %d", node.bootTime)
		}
	})

	t.Run("destructive difference stops the update", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		outcome := ApplyConfigurationUpdate(t.Context(), configurationUpdate{
			node:       node,
			strategy:   bootstrapv1alpha1.ConfigurationApplyStrategyStagedReboot,
			desired:    destructiveDifference,
			rebootGate: func(context.Context) (bool, string) { return true, "" },
		})
		if outcome.FailureMessage == "" || node.rebootApplies != 0 {
			t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, reboot applies = %d", outcome, node.rebootApplies)
		}
	})

	t.Run("completion requires Talos health and Node readiness", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100, servicesErr: errors.New("kubelet is not healthy")}
		updater := configurationUpdate{
			node:      node,
			strategy:  bootstrapv1alpha1.ConfigurationApplyStrategyStagedReboot,
			desired:   active,
			nodeReady: func(context.Context) (bool, string) { return true, "" },
		}
		if outcome := ApplyConfigurationUpdate(t.Context(), updater); outcome.Done || outcome.RetryMessage == "" {
			t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, want a retry while Talos is unhealthy", outcome)
		}
		node.servicesErr = nil
		updater.nodeReady = func(context.Context) (bool, string) { return false, "The Node is not Ready yet." }
		if outcome := ApplyConfigurationUpdate(t.Context(), updater); outcome.Done {
			t.Fatal("ApplyConfigurationUpdate() completed while the Node was not Ready")
		}
		updater.nodeReady = func(context.Context) (bool, string) { return true, "" }
		if outcome := ApplyConfigurationUpdate(t.Context(), updater); !outcome.Done {
			t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, want completion", outcome)
		}
	})
}

func TestApplyConfigurationUpdateStopsWhenInputsAreUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func() ConfigurationUpdateOutcome
		want string
	}{
		{
			name: "node",
			call: func() ConfigurationUpdateOutcome {
				return ApplyConfigurationUpdate(t.Context(), configurationUpdate{desired: []byte("desired")})
			},
			want: "The desired machine configuration is unavailable",
		},
		{
			name: "desired configuration",
			call: func() ConfigurationUpdateOutcome {
				return ApplyConfigurationUpdate(t.Context(), configurationUpdate{node: &fakeUpdateNode{}})
			},
			want: "The desired machine configuration is unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			outcome := tt.call()
			if outcome.FailureMessage == "" || !strings.Contains(outcome.FailureMessage, tt.want) {
				t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, want failure containing %q", outcome, tt.want)
			}
		})
	}
}

func TestApplyConfigurationUpdateRetriesWhenActiveConfigurationCannotBeObserved(t *testing.T) {
	t.Parallel()

	updater := configurationUpdate{
		node:     &fakeUpdateNode{activeErr: errors.New("Talos API unavailable")},
		desired:  []byte("desired"),
		strategy: bootstrapv1alpha1.ConfigurationApplyStrategyApplyOnly,
	}
	outcome := ApplyConfigurationUpdate(t.Context(), updater)
	if outcome.RetryMessage == "" || outcome.FailureMessage != "" {
		t.Fatalf("ApplyConfigurationUpdate() outcome = %+v, want a retry", outcome)
	}
}

func TestPlanBootstrapConfigPatch(t *testing.T) {
	t.Parallel()

	bootstrapObject := func(secretName, policy string) runtime.RawExtension {
		object := `{"apiVersion":"` + bootstrapv1alpha1.GroupVersion.String() + `","kind":"TartBootstrapConfig","spec":{"configPatchesSecretRef":{"name":"` + secretName + `"}`
		if policy != "" {
			object += `,"updatePolicy":{"configuration":"` + policy + `"}`
		}
		return runtime.RawExtension{Raw: []byte(object + `}}`)}
	}

	tests := map[string]struct {
		current   runtime.RawExtension
		desired   runtime.RawExtension
		wantPatch bool
		wantErr   bool
	}{
		"no difference": {
			current: bootstrapObject("patches-a", ""),
			desired: bootstrapObject("patches-a", ""),
		},
		"raw patch change under the default policy": {
			current:   bootstrapObject("patches-a", ""),
			desired:   bootstrapObject("patches-b", ""),
			wantPatch: true,
		},
		"raw patch change under the ApplyOnly strategy": {
			current:   bootstrapObject("patches-a", "ApplyOnly"),
			desired:   bootstrapObject("patches-b", "ApplyOnly"),
			wantPatch: true,
		},
		"unknown strategy": {
			current: bootstrapObject("patches-a", "Whatever"),
			desired: bootstrapObject("patches-b", "Whatever"),
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			patch, err := planBootstrapConfigPatch(tt.current, tt.desired)
			if tt.wantErr {
				if err == nil {
					t.Fatal("planBootstrapConfigPatch() accepted a change that must be stopped")
				}
				return
			}
			if err != nil {
				t.Fatalf("planBootstrapConfigPatch() error = %v", err)
			}
			if (len(patch.Patch) > 0) != tt.wantPatch {
				t.Fatalf("planBootstrapConfigPatch() patch = %q, want patch = %v", patch.Patch, tt.wantPatch)
			}
		})
	}
}
