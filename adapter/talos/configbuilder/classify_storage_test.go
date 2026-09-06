package configbuilder

import (
	"strings"
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	domainupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/update"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
)

const classifyStorageUserVolumeDocument = `apiVersion: v1alpha1
kind: UserVolumeConfig
name: longhorn-data
provisioning:
    diskSelector:
        match: disk.serial == "disk-b"
    minSize: 10GiB
    maxSize: 20GiB
`

const classifyStorageLVMDocument = `apiVersion: v1alpha1
kind: LVMVolumeGroupConfig
name: topolvm-vg
provisioning:
    volumeSelector:
        match: disk.serial == "disk-c" || disk.serial == "disk-d"
`

// classifyStorageActiveConfigurationは、install target、UserVolumeConfig、LVMVolumeGroupConfigを
// 含む完全なmachine configurationを生成する。各テストはこれをbyte単位で書き換えてdesiredを作り、
// ClassifyConfigurationChangeがstorage documentの変更だけを検出できることを確認する。
func classifyStorageActiveConfiguration(t *testing.T) []byte {
	t.Helper()
	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	installDisk := domainbootstrap.DiskIdentity{DevicePath: "/dev/vda", SizeBytes: 64 * 1024 * 1024 * 1024, Serial: "disk-a", Transport: "virtio"}
	configuration, err := GenerateMachineConfiguration(usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "cluster-a",
		ControlPlaneEndpoint: "https://192.0.2.10:6443",
		KubernetesVersion:    "1.34.0",
		MachineRole:          domainbootstrap.MachineRoleWorker,
		SecretsBundle:        bundle,
		InstallDisk:          &installDisk,
	}, []byte(classifyStorageUserVolumeDocument), []byte(classifyStorageLVMDocument))
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	return configuration
}

func replaceOnce(t *testing.T, configuration []byte, old, replacement string) []byte {
	t.Helper()
	text := string(configuration)
	if !strings.Contains(text, old) {
		t.Fatalf("configuration does not contain %q", old)
	}
	return []byte(strings.Replace(text, old, replacement, 1))
}

// TestClassifyConfigurationChangeStorageSelectorChangeIsDestructiveは、既存UserVolumeの
// disk selectorを変更するとdataを破壊し得るため、in-place updateではなくreprovisionを
// 要求することを検証する。
func TestClassifyConfigurationChangeStorageSelectorChangeIsDestructive(t *testing.T) {
	t.Parallel()

	active := classifyStorageActiveConfiguration(t)
	desired := replaceOnce(t, active, `disk.serial == "disk-b"`, `disk.serial == "disk-c"`)

	class, reason, err := ClassifyConfigurationChange(active, desired)
	if err != nil {
		t.Fatalf("ClassifyConfigurationChange() error = %v", err)
	}
	if class != domainupdate.ChangeReprovisionRequired {
		t.Fatalf("ClassifyConfigurationChange() class = %v, want ChangeReprovisionRequired (reason=%q)", class, reason)
	}
}

// TestClassifyConfigurationChangeStorageShrinkIsDestructiveは、既存UserVolumeのmaxSizeを
// 縮小する変更がreprovisionを要求することを検証する。
func TestClassifyConfigurationChangeStorageShrinkIsDestructive(t *testing.T) {
	t.Parallel()

	active := classifyStorageActiveConfiguration(t)
	desired := replaceOnce(t, active, "maxSize: 20GiB", "maxSize: 15GiB")

	class, reason, err := ClassifyConfigurationChange(active, desired)
	if err != nil {
		t.Fatalf("ClassifyConfigurationChange() error = %v", err)
	}
	if class != domainupdate.ChangeReprovisionRequired {
		t.Fatalf("ClassifyConfigurationChange() class = %v, want ChangeReprovisionRequired (reason=%q)", class, reason)
	}
}

// TestClassifyConfigurationChangeStorageGrowIsSafeは、既存UserVolumeのmaxSizeを拡大する変更が
// dataを保持したままin-place updateとして適用できることを検証する。
func TestClassifyConfigurationChangeStorageGrowIsSafe(t *testing.T) {
	t.Parallel()

	active := classifyStorageActiveConfiguration(t)
	desired := replaceOnce(t, active, "maxSize: 20GiB", "maxSize: 40GiB")

	class, reason, err := ClassifyConfigurationChange(active, desired)
	if err != nil {
		t.Fatalf("ClassifyConfigurationChange() error = %v", err)
	}
	if class != domainupdate.ChangeUpdatable {
		t.Fatalf("ClassifyConfigurationChange() class = %v, want ChangeUpdatable (reason=%q)", class, reason)
	}
}

// TestClassifyConfigurationChangeStorageRemovalIsDestructiveは、既存storage documentの削除が
// reprovisionを要求することを検証する。
func TestClassifyConfigurationChangeStorageRemovalIsDestructive(t *testing.T) {
	t.Parallel()

	active := classifyStorageActiveConfiguration(t)
	text := string(active)
	documents := strings.Split(text, "\n---\n")
	kept := make([]string, 0, len(documents))
	for _, document := range documents {
		if strings.Contains(document, "kind: UserVolumeConfig") {
			continue
		}
		kept = append(kept, document)
	}
	desired := []byte(strings.Join(kept, "\n---\n"))

	class, reason, err := ClassifyConfigurationChange(active, desired)
	if err != nil {
		t.Fatalf("ClassifyConfigurationChange() error = %v", err)
	}
	if class != domainupdate.ChangeReprovisionRequired {
		t.Fatalf("ClassifyConfigurationChange() class = %v, want ChangeReprovisionRequired (reason=%q)", class, reason)
	}
}

// TestClassifyConfigurationChangeLVMVolumeGroupSelectorChangeIsDestructiveは、
// LVMVolumeGroupConfigのphysical volume selector変更がreprovisionを要求することを検証する。
func TestClassifyConfigurationChangeLVMVolumeGroupSelectorChangeIsDestructive(t *testing.T) {
	t.Parallel()

	active := classifyStorageActiveConfiguration(t)
	desired := replaceOnce(t, active, `disk.serial == "disk-c" || disk.serial == "disk-d"`, `disk.serial == "disk-c"`)

	class, reason, err := ClassifyConfigurationChange(active, desired)
	if err != nil {
		t.Fatalf("ClassifyConfigurationChange() error = %v", err)
	}
	if class != domainupdate.ChangeReprovisionRequired {
		t.Fatalf("ClassifyConfigurationChange() class = %v, want ChangeReprovisionRequired (reason=%q)", class, reason)
	}
}
