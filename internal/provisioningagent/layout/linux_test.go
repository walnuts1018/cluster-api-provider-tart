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

package layout

import (
	"strings"
	"testing"
)

func TestSFDiskRoundTripContract(t *testing.T) {
	t.Parallel()

	planned, err := Plan(MinimumDiskSizeBytes, 512)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	script, err := renderSFDiskScript(planned)
	if err != nil {
		t.Fatalf("renderSFDiskScript() error = %v", err)
	}
	for _, partition := range planned.Partitions {
		want := "type=" + partition.TypeGUID + ", name=\"" + partition.Label + "\""
		if !strings.Contains(script, want) {
			t.Errorf("script does not contain %q:\n%s", want, script)
		}
	}
	if strings.Count(script, "start=") != len(profileDefinitions) {
		t.Fatalf("partition lines = %d, want %d", strings.Count(script, "start="), len(profileDefinitions))
	}

	jsonDocument := `{
		"partitiontable": {
			"label": "gpt",
			"sectorsize": 512,
			"partitions": [
				{"node":"/dev/vda1","start":2048,"size":1048576,"type":"c12a7328-f81f-11d2-ba4b-00a0c93ec93b","uuid":"uuid-1","name":"tart-boot"}
			]
		}
	}`
	observed, err := parseSFDiskJSON([]byte(jsonDocument))
	if err != nil {
		t.Fatalf("parseSFDiskJSON() error = %v", err)
	}
	if observed.TableType != "gpt" || observed.SectorSize != 512 || len(observed.Partitions) != 1 {
		t.Fatalf("parseSFDiskJSON() = %#v", observed)
	}
	partition := observed.Partitions[0]
	if partition.DevicePath != "/dev/vda1" || partition.PARTUUID != "uuid-1" || partition.Label != "tart-boot" {
		t.Fatalf("partition = %#v", partition)
	}
}
