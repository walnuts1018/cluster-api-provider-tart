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
	"errors"
	"strconv"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestPlanCreatesDeterministicAlignedLayout(t *testing.T) {
	t.Parallel()

	for _, sectorSize := range []int64{512, 4096} {
		t.Run(strconv.FormatInt(sectorSize, 10), func(t *testing.T) {
			t.Parallel()
			planned, err := Plan(MinimumDiskSizeBytes, sectorSize)
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			if len(planned.Partitions) != len(profileDefinitions) {
				t.Fatalf("len(Partitions) = %d, want %d", len(planned.Partitions), len(profileDefinitions))
			}
			for index, partition := range planned.Partitions {
				if partition.Number != index+1 {
					t.Errorf("Partitions[%d].Number = %d, want %d", index, partition.Number, index+1)
				}
				if partition.StartSector*sectorSize%alignmentBytes != 0 {
					t.Errorf("Partitions[%d] is not 1 MiB aligned", index)
				}
				if partition.SizeSectors*sectorSize < partition.MinimumSizeBytes {
					t.Errorf("Partitions[%d] is smaller than minimum", index)
				}
			}
			data := planned.Partitions[len(planned.Partitions)-1]
			if !data.Grow || data.SizeSectors*sectorSize <= data.MinimumSizeBytes {
				t.Fatalf("Data size = %d, want more than minimum %d", data.SizeSectors*sectorSize, data.MinimumSizeBytes)
			}
		})
	}
}

func TestPlanRejectsUnsafeDiskGeometry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		diskBytes  int64
		sectorSize int64
	}{
		{name: "64GiB未満", diskBytes: MinimumDiskSizeBytes - 1, sectorSize: 512},
		{name: "sector sizeが0", diskBytes: MinimumDiskSizeBytes, sectorSize: 0},
		{name: "未対応sector size", diskBytes: MinimumDiskSizeBytes, sectorSize: 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Plan(tt.diskBytes, tt.sectorSize)
			if !errors.Is(err, ErrInvalidLayout) {
				t.Fatalf("Plan() error = %v, want ErrInvalidLayout", err)
			}
		})
	}
}

func TestResolveMapsEveryDiskRole(t *testing.T) {
	t.Parallel()

	observed := observedFromPlan(t)
	resolved, err := Resolve(observed)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	for _, definition := range profileDefinitions {
		device, ok := resolved[definition.Role]
		if !ok {
			t.Errorf("role %s was not resolved", definition.Role)
			continue
		}
		if device.Role != definition.Role || device.DevicePath == "" || device.PARTUUID == "" {
			t.Errorf("role %s resolved to %#v", definition.Role, device)
		}
	}
}

func TestResolveRejectsAmbiguousOrModifiedLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*ObservedLayout)
	}{
		{name: "GPT以外", mutate: func(layout *ObservedLayout) { layout.TableType = "dos" }},
		{name: "未対応sector size", mutate: func(layout *ObservedLayout) { layout.SectorSize = 1024 }},
		{name: "partition不足", mutate: func(layout *ObservedLayout) {
			layout.Partitions = layout.Partitions[:len(layout.Partitions)-1]
		}},
		{name: "label重複", mutate: func(layout *ObservedLayout) {
			layout.Partitions[1].Label = layout.Partitions[0].Label
		}},
		{name: "type GUID不一致", mutate: func(layout *ObservedLayout) {
			layout.Partitions[1].TypeGUID = linuxFilesystemType
		}},
		{name: "固定partition拡大", mutate: func(layout *ObservedLayout) {
			layout.Partitions[1].SizeSectors++
		}},
		{name: "物理順序不一致", mutate: func(layout *ObservedLayout) {
			layout.Partitions[2].StartSector = layout.Partitions[0].StartSector
		}},
		{name: "partition間gap", mutate: func(layout *ObservedLayout) {
			layout.Partitions[2].StartSector += 2048
		}},
		{name: "PARTUUID欠落", mutate: func(layout *ObservedLayout) {
			layout.Partitions[0].PARTUUID = ""
		}},
		{name: "PARTUUID重複", mutate: func(layout *ObservedLayout) {
			layout.Partitions[1].PARTUUID = layout.Partitions[0].PARTUUID
		}},
		{name: "alignment不一致", mutate: func(layout *ObservedLayout) {
			layout.Partitions[0].StartSector++
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			observed := observedFromPlan(t)
			tt.mutate(&observed)
			_, err := Resolve(observed)
			if !errors.Is(err, ErrInvalidLayout) {
				t.Fatalf("Resolve() error = %v, want ErrInvalidLayout", err)
			}
		})
	}
}

func observedFromPlan(t *testing.T) ObservedLayout {
	t.Helper()
	planned, err := Plan(MinimumDiskSizeBytes, 512)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	partitions := make([]ObservedPartition, 0, len(planned.Partitions))
	for _, partition := range planned.Partitions {
		partitions = append(partitions, ObservedPartition{
			DevicePath:  "/dev/test" + string(rune('0'+partition.Number)),
			Label:       partition.Label,
			TypeGUID:    partition.TypeGUID,
			PARTUUID:    "partuuid-" + string(rune('0'+partition.Number)),
			StartSector: partition.StartSector,
			SizeSectors: partition.SizeSectors,
		})
	}
	return ObservedLayout{
		TableType:  "gpt",
		SectorSize: planned.SectorSize,
		Partitions: partitions,
	}
}

func TestDefinitionsCannotMutateProfile(t *testing.T) {
	t.Parallel()

	definitions := Definitions()
	definitions[0].Role = agentprotocol.DiskRoleData
	if Definitions()[0].Role != agentprotocol.DiskRoleBoot {
		t.Fatal("Definitions() exposed mutable profile storage")
	}
}
