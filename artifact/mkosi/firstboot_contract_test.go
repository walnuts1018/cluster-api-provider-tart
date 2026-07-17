// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mkosi

import (
	"os"
	"strings"
	"testing"
)

func TestFirstBootUnitはkubelet開始前にBootstrapとBootReportを実行する(t *testing.T) {
	unit := readText(t, "mkosi.extra/usr/lib/systemd/system/tart-first-boot.service")

	for _, want := range []string{
		"Wants=network-online.target",
		"After=network-online.target",
		"Before=kubelet.service",
		"ExecStart=/usr/libexec/tart/first-boot",
		"StandardOutput=journal+console",
		"StandardError=journal+console",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("tart-first-boot.service does not contain %q\n%s", want, unit)
		}
	}
}

func TestFirstBootScriptはBootstrap適用後にMarkerを使ってBootReportを送る(t *testing.T) {
	script := readText(t, "mkosi.extra/usr/libexec/tart/first-boot")

	applyIndex := strings.Index(script, "--apply-bootstrap-only")
	reportIndex := strings.Index(script, "--report-boot-only")
	if applyIndex < 0 || reportIndex < 0 || applyIndex > reportIndex {
		t.Fatalf("first-boot must apply Bootstrap before reporting boot\n%s", script)
	}
	for _, want := range []string{
		"/proc/sys/kernel/random/boot_id",
		"--state-dir=\"$state_dir\"",
		"--bootstrap-work-dir=\"$bootstrap_work_dir\"",
		"--system-uuid=\"$system_uuid\"",
		"--active-slot=\"$active_slot\"",
		"--artifact-generation=\"$artifact_generation\"",
		"TART_QEMU_ROOT_SOURCE=",
		"TART_QEMU_ROOT_OPTIONS=",
		"TART_QEMU_ROOT_READ_ONLY=",
		"--state-mounted",
		"--data-mounted",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("first-boot does not contain %q\n%s", want, script)
		}
	}
}

func TestCloudConfigAdapterはCABPKPayloadをNoCloudDatasourceとして適用する(t *testing.T) {
	script := readText(t, "mkosi.extra/usr/libexec/tart/apply-cloud-config")

	for _, want := range []string{
		"cloud-init schema --config-file \"$payload_path\"",
		"$seed_dir/user-data",
		"instance-id: tart-$machine_id",
		"cloud-init clean --logs",
		"cloud-init init",
		"cloud-init modules --mode=config",
		"cloud-init modules --mode=final",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("apply-cloud-config does not contain %q\n%s", want, script)
		}
	}
}

func TestRepartVerityHashはDataPartitionの最小化設定と対応する(t *testing.T) {
	root := readText(t, "mkosi.repart/10-root.conf")
	verity := readText(t, "mkosi.repart/20-root-verity.conf")

	if !strings.Contains(root, "Verity=data") || !strings.Contains(root, "VerityMatchKey=root") {
		t.Fatalf("root partition must be paired with the verity hash partition\n%s", root)
	}
	if !strings.Contains(verity, "Verity=hash") || !strings.Contains(verity, "VerityMatchKey=root") {
		t.Fatalf("verity partition must be paired with the root data partition\n%s", verity)
	}
	if strings.Contains(verity, "Minimize=") &&
		!strings.Contains(root, "Minimize=") &&
		!strings.Contains(root, "CopyBlocks=") {
		t.Fatalf("verity hash partition minimization requires root partition Minimize or CopyBlocks\nroot:\n%s\nverity:\n%s", root, verity)
	}
	if strings.Contains(root, "Minimize=best") {
		t.Fatalf("root partition uses ext4, so Minimize=best is not supported\n%s", root)
	}
	if !strings.Contains(root, "Minimize=guess") {
		t.Fatalf("root partition must use Minimize=guess for verity hash generation\n%s", root)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
