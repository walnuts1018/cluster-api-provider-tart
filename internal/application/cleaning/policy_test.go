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

package cleaning

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestAllowedTargetRolesはDeletionPolicyごとの破壊範囲を固定する(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy infrastructurev1beta1.DeletionPolicy
		want   []agentprotocol.DiskRole
	}{
		{
			name:   "WipeAll",
			policy: infrastructurev1beta1.DeletionPolicyWipeAll,
			want: []agentprotocol.DiskRole{
				agentprotocol.DiskRoleBoot,
				agentprotocol.DiskRoleOSA,
				agentprotocol.DiskRoleOSB,
				agentprotocol.DiskRoleVerityA,
				agentprotocol.DiskRoleVerityB,
				agentprotocol.DiskRoleState,
				agentprotocol.DiskRoleData,
			},
		},
		{
			name:   "RetainData",
			policy: infrastructurev1beta1.DeletionPolicyRetainData,
			want: []agentprotocol.DiskRole{
				agentprotocol.DiskRoleBoot,
				agentprotocol.DiskRoleOSA,
				agentprotocol.DiskRoleOSB,
				agentprotocol.DiskRoleVerityA,
				agentprotocol.DiskRoleVerityB,
				agentprotocol.DiskRoleState,
			},
		},
		{
			name:   "RetainState",
			policy: infrastructurev1beta1.DeletionPolicyRetainState,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AllowedTargetRoles(tt.policy)
			if err != nil {
				t.Fatalf("AllowedTargetRoles() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len(AllowedTargetRoles()) = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for index := range got {
				if got[index] != tt.want[index] {
					t.Fatalf("AllowedTargetRoles()[%d] = %q, want %q", index, got[index], tt.want[index])
				}
			}
		})
	}
}

func TestWipeAllDeadlineは観測ディスク容量に応じて伸びる(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sizeBytes int64
		want      time.Duration
	}{
		{name: "unknown size uses minimum", sizeBytes: 0, want: 2 * time.Hour},
		{name: "small disk uses minimum", sizeBytes: 64 * gibibyte, want: 2 * time.Hour},
		{name: "one tebibyte adds overwrite budget", sizeBytes: tebibyte, want: 4*time.Hour + 20*time.Minute},
		{name: "four tebibytes keeps scaling", sizeBytes: 4 * tebibyte, want: 10*time.Hour + 20*time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := WipeAllDeadline(tt.sizeBytes); got != tt.want {
				t.Fatalf("WipeAllDeadline(%d) = %s, want %s", tt.sizeBytes, got, tt.want)
			}
		})
	}
}

func TestBuildOperationDraftはWipeAll削除時に容量連動deadlineを設定する(t *testing.T) {
	t.Parallel()

	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			RootDeviceHints: infrastructurev1beta1.RootDeviceHints{
				MinSizeBytes: tebibyte,
			},
		},
		Status: infrastructurev1beta1.TartHostStatus{
			Inventory: infrastructurev1beta1.HostInventory{
				RootDisk: infrastructurev1beta1.ObservedDisk{SizeBytes: 4 * tebibyte},
			},
		},
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
		},
	}
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)

	operation, err := BuildOperationDraft(machine, host, "", now)
	if err != nil {
		t.Fatalf("BuildOperationDraft() error = %v", err)
	}
	if operation.Spec.Type != infrastructurev1beta1.OperationTypeWipeAll {
		t.Fatalf("operation type = %q, want WipeAll", operation.Spec.Type)
	}
	wantDeadline := metav1.NewTime(now.Add(10*time.Hour + 20*time.Minute))
	if !operation.Spec.Deadline.Equal(&wantDeadline) {
		t.Fatalf("deadline = %s, want %s", operation.Spec.Deadline, wantDeadline)
	}
}
