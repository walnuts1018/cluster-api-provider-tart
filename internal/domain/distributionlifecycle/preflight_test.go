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

package distributionlifecycle

import "testing"

func TestPreflightはminorVersionSkipを拒否する(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		wantErr bool
	}{
		{name: "patch更新", current: "v1.34.1", target: "v1.34.2"},
		{name: "minorを1つ上げる", current: "v1.34.4", target: "v1.35.0"},
		{name: "minorを2つ上げる", current: "v1.34.9", target: "v1.36.0", wantErr: true},
		{name: "downgrade", current: "v1.35.0", target: "v1.34.0", wantErr: true},
		{name: "major更新", current: "v1.35.0", target: "v2.0.0", wantErr: true},
		{name: "不正なversion", current: "v1.35.0", target: "latest", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Preflight(PreflightInput{
				Distribution:   DistributionKubeadm,
				CurrentVersion: tt.current,
				TargetVersion:  tt.target,
				UpdateClass:    UpdateClassKubernetesBinary,
				NodeRole:       NodeRoleWorker,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Preflight() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestPreflightはWorkerがControlPlaneより先に進むことを拒否する(t *testing.T) {
	err := Preflight(PreflightInput{
		Distribution:                    DistributionKubeadm,
		CurrentVersion:                  "v1.34.0",
		TargetVersion:                   "v1.35.0",
		ControlPlaneAcceptedVersion:     "v1.34.0",
		RequireControlPlaneTargetAccept: true,
		UpdateClass:                     UpdateClassKubernetesBinary,
		NodeRole:                        NodeRoleWorker,
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want worker ordering error")
	}
}

func TestPreflightはStateMigrationでSnapshotRefを必須にする(t *testing.T) {
	err := Preflight(PreflightInput{
		Distribution:   DistributionKubeadm,
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    UpdateClassStateMigration,
		NodeRole:       NodeRoleControlPlane,
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want snapshot requirement error")
	}

	err = Preflight(PreflightInput{
		Distribution:   DistributionKubeadm,
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    UpdateClassStateMigration,
		NodeRole:       NodeRoleControlPlane,
		SnapshotRef:    "etcd-snapshot-1",
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
}
