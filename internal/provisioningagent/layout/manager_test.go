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
	"context"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type recordingDiskIO struct {
	logicalSectorSizeCalls int
	createCalls            int
	readCalls              int
	observed               ObservedLayout
}

func (diskIO *recordingDiskIO) LogicalSectorSize(context.Context, string) (int64, error) {
	diskIO.logicalSectorSizeCalls++
	return 512, nil
}

func (diskIO *recordingDiskIO) CreatePartitionTable(context.Context, string, PlannedLayout) error {
	diskIO.createCalls++
	return nil
}

func (diskIO *recordingDiskIO) ReadPartitionTable(context.Context, string) (ObservedLayout, error) {
	diskIO.readCalls++
	return diskIO.observed, nil
}

func TestManagerCreatesPartitionTableOnlyForProvision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		operation         agentprotocol.OperationType
		wantGeometryCalls int
		wantCreateCalls   int
	}{
		{name: "Provision", operation: agentprotocol.OperationTypeProvision, wantGeometryCalls: 1, wantCreateCalls: 1},
		{name: "Update", operation: agentprotocol.OperationTypeUpdate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diskIO := &recordingDiskIO{observed: observedFromPlan(t)}
			manager := NewManager(diskIO)
			resolved, err := manager.Prepare(t.Context(), tt.operation, disk.Device{
				Path:      "/dev/test",
				SizeBytes: MinimumDiskSizeBytes,
			})
			if err != nil {
				t.Fatalf("Prepare() error = %v", err)
			}
			if len(resolved) != len(profileDefinitions) {
				t.Fatalf("len(resolved) = %d, want %d", len(resolved), len(profileDefinitions))
			}
			if diskIO.logicalSectorSizeCalls != tt.wantGeometryCalls {
				t.Errorf("LogicalSectorSize() calls = %d, want %d", diskIO.logicalSectorSizeCalls, tt.wantGeometryCalls)
			}
			if diskIO.createCalls != tt.wantCreateCalls {
				t.Errorf("CreatePartitionTable() calls = %d, want %d", diskIO.createCalls, tt.wantCreateCalls)
			}
			if diskIO.readCalls != 1 {
				t.Errorf("ReadPartitionTable() calls = %d, want 1", diskIO.readCalls)
			}
		})
	}
}
