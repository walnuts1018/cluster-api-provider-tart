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

// RemovalObservationはmemberを一つ削除してもquorumを維持できるか判定するためのetcd membershipとhealthの観測値だけを持つ。
type RemovalObservation struct {
	MemberCount          int
	HealthyMemberCount   int
	TargetHealthy        bool
	TargetHealthObserved bool
}

// CanRemoveMemberは現在と削除後のmember集合がともにhealthy memberの過半数を持つ場合だけtrueを返す。対象のhealth不明と観測値の矛盾はavailability policyに関係なく安全ではないためfalseを返す。
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
