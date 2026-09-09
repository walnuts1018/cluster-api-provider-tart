package host

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"errors"
	"slices"

	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

var ErrNoEligibleHost = errors.New("no eligible host")

// SelectFreshは新規allocation用の候補をname順で一つ選ぶ。RetainedやReusableを通常のMachine allocationへ混ぜないことで、data保持中のHostを暗黙に再利用しない。
func SelectFresh(hosts []infrav1alpha1.TartHost, selector *infrav1alpha1.HostSelector) (*infrav1alpha1.TartHost, error) {
	return SelectFreshForFailureDomain(hosts, selector, "")
}

// SelectFreshForFailureDomainは指定されたFailure Domainに属するfresh Hostをname順で一つ選ぶ。空のFailure Domainは制約なしとして扱う。
func SelectFreshForFailureDomain(hosts []infrav1alpha1.TartHost, selector *infrav1alpha1.HostSelector, failureDomain string) (*infrav1alpha1.TartHost, error) {
	return SelectFreshForFailureDomainWithRendezvous(hosts, selector, failureDomain, "")
}

// SelectFreshForFailureDomainWithRendezvousは、machineUIDを用いたrendezvous hashingで候補を分散させつつfresh Hostを一つ選ぶ。machineUIDが空の場合は従来のname順にフォールバックする。
func SelectFreshForFailureDomainWithRendezvous(hosts []infrav1alpha1.TartHost, selector *infrav1alpha1.HostSelector, failureDomain string, machineUID types.UID) (*infrav1alpha1.TartHost, error) {
	candidates := make([]infrav1alpha1.TartHost, 0, len(hosts))
	for _, candidate := range hosts {
		if Classify(candidate.Spec) != hostdomain.Available || candidate.Spec.HostID.IsZero() {
			continue
		}
		if !MatchesForFailureDomain(candidate.Labels, candidate.Spec, selector, failureDomain) {
			continue
		}
		candidates = append(candidates, *candidate.DeepCopy())
	}
	if len(candidates) == 0 {
		return nil, ErrNoEligibleHost
	}
	if machineUID == "" {
		slices.SortFunc(candidates, func(left, right infrav1alpha1.TartHost) int {
			return cmp.Compare(left.Name, right.Name)
		})
		return &candidates[0], nil
	}
	slices.SortFunc(candidates, func(left, right infrav1alpha1.TartHost) int {
		leftScore := hostScore(machineUID, left.Spec.HostID)
		rightScore := hostScore(machineUID, right.Spec.HostID)
		if diff := bytes.Compare(leftScore[:], rightScore[:]); diff != 0 {
			return -diff // 高スコアを先頭に
		}
		return cmp.Compare(left.Name, right.Name)
	})
	return &candidates[0], nil
}

func hostScore(machineUID types.UID, hostID hostdomain.HostID) [32]byte {
	h := sha256.Sum256([]byte(string(machineUID) + "\x00" + hostID.String()))
	return h
}
