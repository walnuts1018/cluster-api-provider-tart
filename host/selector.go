package host

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// Matches reports whether a Host satisfies a Machine's hostSelector. A nil selector
// matches every Available Host; callers are responsible for filtering by Eligibility
// separately, since selector matching and allocation eligibility are independent
// observations.
func Matches(hostLabels map[string]string, spec infrav1alpha1.TartHostSpec, selector *infrav1alpha1.HostSelector) bool {
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
