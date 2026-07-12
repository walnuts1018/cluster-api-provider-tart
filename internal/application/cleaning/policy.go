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
	"fmt"
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const (
	gibibyte               = int64(1024 * 1024 * 1024)
	tebibyte               = 1024 * gibibyte
	minimumWipeAllDeadline = 2 * time.Hour
	wipeAllBaseOverhead    = 20 * time.Minute
	wipeAllPerTiB          = 2 * time.Hour
)

// AllowedTargetRoles はDeletionPolicyごとの破壊可能範囲をPlanへ写像する。
func AllowedTargetRoles(policy infrastructurev1beta1.DeletionPolicy) ([]agentprotocol.DiskRole, error) {
	switch policy {
	case infrastructurev1beta1.DeletionPolicyWipeAll:
		return []agentprotocol.DiskRole{
			agentprotocol.DiskRoleBoot,
			agentprotocol.DiskRoleOSA,
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityA,
			agentprotocol.DiskRoleVerityB,
			agentprotocol.DiskRoleState,
			agentprotocol.DiskRoleData,
		}, nil
	case infrastructurev1beta1.DeletionPolicyRetainData:
		return []agentprotocol.DiskRole{
			agentprotocol.DiskRoleBoot,
			agentprotocol.DiskRoleOSA,
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityA,
			agentprotocol.DiskRoleVerityB,
			agentprotocol.DiskRoleState,
		}, nil
	case infrastructurev1beta1.DeletionPolicyRetainState:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported deletion policy %q", policy)
	}
}

// WipeAllDeadline は観測済みディスク容量に応じてWipeAllのdeadlineを伸ばす。
func WipeAllDeadline(sizeBytes int64) time.Duration {
	if sizeBytes <= 0 || sizeBytes < tebibyte {
		return minimumWipeAllDeadline
	}
	tebibytes := (sizeBytes + tebibyte - 1) / tebibyte
	return minimumWipeAllDeadline + wipeAllBaseOverhead + time.Duration(tebibytes)*wipeAllPerTiB
}
