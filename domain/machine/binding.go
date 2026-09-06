// Package machineはTartMachineとTartHostの間のbinding判定に関する値オブジェクトと純粋関数を提供する。
// 外部SDK型(controller-runtime、CAPI core、Kubernetes API)への依存を一切持たない。
package machine

import "errors"

// ErrAmbiguousClaimは、同一のMachineをconsumerRefとして指すHostが複数観測された場合に返す。
// このような状態はclaim更新の競合や手動編集を示すため、呼び出し側はfail-closedで停止すべきである。
var ErrAmbiguousClaim = errors.New("machine has ambiguous host claims")

// ConsumerUIDはHostをclaimしているconsumer(TartMachine)のUIDである。
type ConsumerUID string

// IsZeroはUIDが未設定かを返す。
func (u ConsumerUID) IsZero() bool {
	return u == ""
}

// IsBoundToは、HostのconsumerRefが示すUIDが指定されたMachine UIDと一致するかを判定する。
func IsBoundTo(hostConsumerUID, machineUID ConsumerUID) bool {
	return !hostConsumerUID.IsZero() && !machineUID.IsZero() && hostConsumerUID == machineUID
}

// HostClaimCandidateは、claim判定の対象となるHostの名前とconsumer UIDだけを保持する最小限の値である。
type HostClaimCandidate struct {
	Name        string
	ConsumerUID ConsumerUID
}

// FindClaimedHostNameは、指定されたMachine UIDをconsumerRefとして持つHostを候補から一意に決定する。
// 一致するHostが存在しない場合は空文字列を返し、複数存在する場合はErrAmbiguousClaimを返す。
// この判定はHost claimがCASで一意に保たれているという不変条件の健全性チェックであり、
// 複数一致は手動編集や過去の不整合を示すためclaim解除を進めずに停止する。
func FindClaimedHostName(candidates []HostClaimCandidate, machineUID ConsumerUID) (string, error) {
	var claimed string
	for _, candidate := range candidates {
		if !IsBoundTo(candidate.ConsumerUID, machineUID) {
			continue
		}
		if claimed != "" && claimed != candidate.Name {
			return "", ErrAmbiguousClaim
		}
		claimed = candidate.Name
	}
	return claimed, nil
}
