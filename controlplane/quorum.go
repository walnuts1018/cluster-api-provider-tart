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
