package controlplane

import domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"

// RemovalObservationはdomain/controlplaneの同名型へのエイリアスである。旧importパスからの移行期間中の互換性のため保持する。
type RemovalObservation = domaincontrolplane.RemovalObservation

// CanRemoveMemberはdomain/controlplaneの純粋関数へ委譲する。
func CanRemoveMember(observation RemovalObservation) bool {
	return domaincontrolplane.CanRemoveMember(observation)
}

// CanTemporarilyDisruptMemberはdomain/controlplaneの純粋関数へ委譲する。
func CanTemporarilyDisruptMember(observation RemovalObservation) bool {
	return domaincontrolplane.CanTemporarilyDisruptMember(observation)
}
