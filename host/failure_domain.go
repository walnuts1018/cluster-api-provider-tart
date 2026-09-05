package host

import (
	"maps"
	"slices"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// FailureDomainsは登録済みHostのFailure Domainを重複なく決定論的な順序で返す。HostにFailure Domainごとの能力差を表す情報がないため、観測された全Domainをcontrol-planeで利用可能としてsurfaceする。
func FailureDomains(hosts []infrav1alpha1.TartHost) []clusterv1.FailureDomain {
	names := make(map[string]struct{}, len(hosts))
	for index := range hosts {
		if hosts[index].Spec.FailureDomain != "" {
			names[hosts[index].Spec.FailureDomain] = struct{}{}
		}
	}
	ordered := slices.Sorted(maps.Keys(names))

	result := make([]clusterv1.FailureDomain, 0, len(ordered))
	for _, name := range ordered {
		controlPlane := true
		result = append(result, clusterv1.FailureDomain{Name: name, ControlPlane: &controlPlane})
	}
	return result
}
