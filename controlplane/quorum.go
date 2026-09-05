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

package controlplane

// RemovalObservation contains only observed etcd membership and health needed
// to decide whether one member can be removed without losing quorum.
type RemovalObservation struct {
	MemberCount          int
	HealthyMemberCount   int
	TargetHealthy        bool
	TargetHealthObserved bool
}

// CanRemoveMember returns true only when the current and post-removal member
// sets both have a majority of healthy members. Unknown target health and
// inconsistent observations are unsafe, regardless of an availability policy.
func CanRemoveMember(observation RemovalObservation) bool {
	if observation.MemberCount < 2 || !observation.TargetHealthObserved {
		return false
	}
	if observation.HealthyMemberCount < 0 || observation.HealthyMemberCount > observation.MemberCount {
		return false
	}
	if observation.TargetHealthy && observation.HealthyMemberCount == 0 {
		return false
	}
	if !observation.TargetHealthy && observation.HealthyMemberCount == observation.MemberCount {
		return false
	}
	if observation.HealthyMemberCount < quorumSize(observation.MemberCount) {
		return false
	}

	remainingMembers := observation.MemberCount - 1
	remainingHealthy := observation.HealthyMemberCount
	if observation.TargetHealthy {
		remainingHealthy--
	}

	return remainingHealthy >= quorumSize(remainingMembers)
}

func quorumSize(memberCount int) int {
	return memberCount/2 + 1
}
