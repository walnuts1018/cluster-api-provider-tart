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
		"--active-slot=\"$active_slot\"",
		"--artifact-generation=\"$artifact_generation\"",
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

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
