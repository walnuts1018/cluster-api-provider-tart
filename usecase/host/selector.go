package host

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// MatchesはHostがMachineのhostSelectorを満たすかを返す。nil selectorは全てのAvailable Hostに一致する。selectorの一致とallocation eligibilityは独立した観測であるため、Eligibilityによる絞り込みは呼び出し側が別途行う。
func Matches(hostLabels map[string]string, spec infrav1alpha1.TartHostSpec, selector *infrav1alpha1.HostSelector) bool {
	return MatchesForFailureDomain(hostLabels, spec, selector, "")
}

// MatchesForFailureDomainはHostがhostSelectorとCAPI MachineのFailure Domainを満たすかを返す。Failure DomainはCAPI Machineが所有するため、HostSelectorへ複製しない。
func MatchesForFailureDomain(hostLabels map[string]string, spec infrav1alpha1.TartHostSpec, selector *infrav1alpha1.HostSelector, failureDomain string) bool {
	if failureDomain != "" && spec.FailureDomain != failureDomain {
		return false
	}
	if selector == nil {
		return true
	}
	if selector.Architecture != "" && selector.Architecture != spec.Architecture {
		return false
	}
	hostSelector, err := metav1.LabelSelectorAsSelector(&selector.Selector)
	if err != nil {
		return false
	}
	return hostSelector.Matches(labels.Set(hostLabels))
}
