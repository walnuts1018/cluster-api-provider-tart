package bootstrap

import (
	"errors"
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	talosmachine "github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
)

func TestSelectInstallDisk(t *testing.T) {
	t.Parallel()

	base := InstallDisk{
		DevicePath: "/dev/vda",
		SizeBytes:  64 * 1024 * 1024 * 1024,
		Model:      "TART DISK",
		Serial:     "disk-a",
		WWID:       "wwid-a",
		BusPath:    "pci-0000:00:05.0",
		Transport:  "virtio",
	}

	tests := map[string]struct {
		disks []InstallDisk
		want  InstallDisk
		err   error
	}{
		"unique stable disk": {
			disks: []InstallDisk{base, {DevicePath: "/dev/vdb", SizeBytes: 128 * 1024 * 1024 * 1024, Model: "TART DATA", Serial: "disk-b", Transport: "virtio"}},
			want:  base,
		},
		"single disk without transport metadata": {
			disks: []InstallDisk{{DevicePath: base.DevicePath, SizeBytes: base.SizeBytes, Serial: base.Serial}},
			want:  InstallDisk{DevicePath: base.DevicePath, SizeBytes: base.SizeBytes, Serial: base.Serial},
		},
		"ambiguous disk identity": {
			disks: []InstallDisk{base, {DevicePath: "/dev/vdb", SizeBytes: base.SizeBytes, Model: base.Model, Serial: base.Serial, WWID: base.WWID, BusPath: base.BusPath, Transport: base.Transport}},
			err:   ErrInstallDiskAmbiguous,
		},
		"no writable disk": {
			disks: []InstallDisk{{DevicePath: "/dev/vda", SizeBytes: base.SizeBytes, Transport: "virtio", ReadOnly: true}},
			err:   ErrInstallDiskUnavailable,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := SelectInstallDisk(tt.disks)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("SelectInstallDisk() error = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectInstallDisk() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SelectInstallDisk() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEnsureInstallDiskAddsUnattendedConfiguration(t *testing.T) {
	t.Parallel()

	disk := InstallDisk{
		DevicePath: "/dev/vda",
		SizeBytes:  64 * 1024 * 1024 * 1024,
		Serial:     "disk-a",
		Transport:  "virtio",
	}
	configuration, err := EnsureInstallDisk([]byte(renderBaseConfiguration), disk)
	if err != nil {
		t.Fatalf("EnsureInstallDisk() error = %v", err)
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		t.Fatalf("configloader.NewFromBytes() error = %v", err)
	}
	unattended := provider.UnattendedInstallConfig()
	if unattended == nil || unattended.VolumeSelector().IsZero() {
		t.Fatal("EnsureInstallDisk() did not add a valid unattended install selector")
	}
	configuration, err = talos.SetInstallerImage(configuration, "v1.14.0", "schematic")
	if err != nil {
		t.Fatalf("SetInstallerImage() error = %v", err)
	}
	provider, err = configloader.NewFromBytes(configuration)
	if err != nil {
		t.Fatalf("configloader.NewFromBytes() after installer patch error = %v", err)
	}
	if got := provider.UnattendedInstallConfig().InstallerImage(); got != "factory.talos.dev/metal-installer/schematic:v1.14.0" {
		t.Fatalf("installer image = %q, want patched image", got)
	}
	configured, err := HasInstallDiskConfiguration(configuration)
	if err != nil {
		t.Fatalf("HasInstallDiskConfiguration() error = %v", err)
	}
	if !configured {
		t.Fatal("HasInstallDiskConfiguration() = false for generated selector")
	}
}

func TestEnsureInstallDiskRejectsTargetForAnotherDisk(t *testing.T) {
	t.Parallel()

	selected := InstallDisk{
		DevicePath: "/dev/vda",
		SizeBytes:  64 * 1024 * 1024 * 1024,
		Serial:     "disk-a",
		Transport:  "virtio",
	}
	configuration, err := EnsureInstallDisk([]byte(renderBaseConfiguration), selected)
	if err != nil {
		t.Fatalf("EnsureInstallDisk() error = %v", err)
	}

	other := InstallDisk{
		DevicePath: "/dev/vdb",
		SizeBytes:  128 * 1024 * 1024 * 1024,
		Serial:     "disk-b",
		Transport:  "virtio",
	}
	if _, err := EnsureInstallDisk(configuration, other); !errors.Is(err, ErrInstallConfigurationInvalid) {
		t.Fatalf("EnsureInstallDisk() error = %v, want ErrInstallConfigurationInvalid", err)
	}
}

func TestGenerateMachineConfigurationIncludesInstallTarget(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	configuration, err := GenerateMachineConfiguration(MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: "https://192.0.2.10:6443",
		KubernetesVersion:    "1.34.0",
		MachineType:          talosmachine.TypeWorker,
		SecretsBundle:        bundle,
		InstallDisk: &InstallDisk{
			DevicePath: "/dev/vda",
			SizeBytes:  64 * 1024 * 1024 * 1024,
			Serial:     "disk-a",
			Transport:  "virtio",
		},
	})
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		t.Fatalf("configloader.NewFromBytes() error = %v", err)
	}
	if unattended := provider.UnattendedInstallConfig(); unattended == nil || unattended.VolumeSelector().IsZero() {
		t.Fatal("GenerateMachineConfiguration() did not include a valid unattended install target")
	}
	configuration, err = talos.SetInstallerImage(configuration, "v1.14.0", "schematic")
	if err != nil {
		t.Fatalf("SetInstallerImage() on generated configuration error = %v", err)
	}
	if _, err := DigestEffectiveConfiguration(configuration); err != nil {
		t.Fatalf("DigestEffectiveConfiguration() rejected generated configuration = %v", err)
	}
}
