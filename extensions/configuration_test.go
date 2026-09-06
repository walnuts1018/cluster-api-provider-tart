package extensions

import (
	"context"
	"errors"
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
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

func (f *fakeUpdateNode) ApplyConfigurationLive(_ context.Context, configuration []byte) error {
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
		InstallDisk: &domainbootstrap.InstallDisk{
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
		outcome := applyConfigurationUpdate(t.Context(), configurationUpdate{
			node:    node,
			policy:  bootstrapv1alpha1.ConfigurationUpdatePolicyLive,
			desired: safeDifference,
		})
		if outcome.retryMessage == "" || outcome.failureMessage != "" {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, want a retry", outcome)
		}
		if node.liveApplies != 1 || node.rebootApplies != 0 {
			t.Fatalf("applyConfigurationUpdate() applied live = %d, reboot = %d", node.liveApplies, node.rebootApplies)
		}
	})

	t.Run("live policy does not fall back to a reboot", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100, liveErr: errors.New("talos rejected the live apply")}
		outcome := applyConfigurationUpdate(t.Context(), configurationUpdate{
			node:    node,
			policy:  bootstrapv1alpha1.ConfigurationUpdatePolicyLive,
			desired: safeDifference,
		})
		if outcome.failureMessage == "" || outcome.done {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, want a failure", outcome)
		}
		if node.rebootApplies != 0 {
			t.Fatalf("applyConfigurationUpdate() fell back to %d reboot applies", node.rebootApplies)
		}
	})

	t.Run("reboot policy waits for the drain gate", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		gateCalls := 0
		outcome := applyConfigurationUpdate(t.Context(), configurationUpdate{
			node:    node,
			policy:  bootstrapv1alpha1.ConfigurationUpdatePolicyReboot,
			desired: safeDifference,
			rebootGate: func(context.Context) (bool, string) {
				gateCalls++
				return false, "The Node drain was blocked."
			},
		})
		if gateCalls != 1 || outcome.retryMessage != "The Node drain was blocked." {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, gate calls = %d", outcome, gateCalls)
		}
		if node.rebootApplies != 0 || node.liveApplies != 0 {
			t.Fatal("applyConfigurationUpdate() applied a configuration while the drain gate was closed")
		}
	})

	t.Run("reboot policy applies after the drain gate", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		outcome := applyConfigurationUpdate(t.Context(), configurationUpdate{
			node:                      node,
			policy:                    bootstrapv1alpha1.ConfigurationUpdatePolicyReboot,
			desired:                   safeDifference,
			rebootGate:                func(context.Context) (bool, string) { return true, "" },
			rebootObservationTimeout:  time.Millisecond,
			rebootObservationInterval: time.Millisecond,
		})
		if outcome.retryMessage == "" || outcome.failureMessage != "" {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, want a retry", outcome)
		}
		if node.rebootApplies != 1 || node.liveApplies != 0 {
			t.Fatalf("applyConfigurationUpdate() applied live = %d, reboot = %d", node.liveApplies, node.rebootApplies)
		}
		if node.bootTime != 101 {
			t.Fatalf("applyConfigurationUpdate() did not observe a reboot, boot time = %d", node.bootTime)
		}
	})

	t.Run("initial only stops without a Machine replacement", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		outcome := applyConfigurationUpdate(t.Context(), configurationUpdate{
			node:    node,
			policy:  bootstrapv1alpha1.ConfigurationUpdatePolicyInitialOnly,
			desired: safeDifference,
		})
		if outcome.failureMessage == "" || node.liveApplies != 0 || node.rebootApplies != 0 {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, applies = %d/%d", outcome, node.liveApplies, node.rebootApplies)
		}
	})

	t.Run("destructive difference stops the update", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100}
		outcome := applyConfigurationUpdate(t.Context(), configurationUpdate{
			node:       node,
			policy:     bootstrapv1alpha1.ConfigurationUpdatePolicyReboot,
			desired:    destructiveDifference,
			rebootGate: func(context.Context) (bool, string) { return true, "" },
		})
		if outcome.failureMessage == "" || node.rebootApplies != 0 {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, reboot applies = %d", outcome, node.rebootApplies)
		}
	})

	t.Run("completion requires Talos health and Node readiness", func(t *testing.T) {
		t.Parallel()
		node := &fakeUpdateNode{active: active, bootTime: 100, servicesErr: errors.New("kubelet is not healthy")}
		updater := configurationUpdate{
			node:      node,
			policy:    bootstrapv1alpha1.ConfigurationUpdatePolicyReboot,
			desired:   active,
			nodeReady: func(context.Context) (bool, string) { return true, "" },
		}
		if outcome := applyConfigurationUpdate(t.Context(), updater); outcome.done || outcome.retryMessage == "" {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, want a retry while Talos is unhealthy", outcome)
		}
		node.servicesErr = nil
		updater.nodeReady = func(context.Context) (bool, string) { return false, "The Node is not Ready yet." }
		if outcome := applyConfigurationUpdate(t.Context(), updater); outcome.done {
			t.Fatal("applyConfigurationUpdate() completed while the Node was not Ready")
		}
		updater.nodeReady = func(context.Context) (bool, string) { return true, "" }
		if outcome := applyConfigurationUpdate(t.Context(), updater); !outcome.done {
			t.Fatalf("applyConfigurationUpdate() outcome = %+v, want completion", outcome)
		}
	})
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
		"raw patch change under the Live policy": {
			current:   bootstrapObject("patches-a", "Live"),
			desired:   bootstrapObject("patches-b", "Live"),
			wantPatch: true,
		},
		"raw patch change under the InitialOnly policy": {
			current: bootstrapObject("patches-a", "InitialOnly"),
			desired: bootstrapObject("patches-b", "InitialOnly"),
			wantErr: true,
		},
		"unknown policy": {
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
