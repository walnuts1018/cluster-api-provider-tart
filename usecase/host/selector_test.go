package host

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestMatchesForFailureDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		hostLabels    map[string]string
		spec          infrav1alpha1.TartHostSpec
		selector      *infrav1alpha1.HostSelector
		failureDomain string
		want          bool
	}{
		{
			name: "nil selector and no failure domain requirement matches everything",
			spec: infrav1alpha1.TartHostSpec{},
			want: true,
		},
		{
			name:          "failure domain mismatch is rejected before selector evaluation",
			spec:          infrav1alpha1.TartHostSpec{FailureDomain: "zone-a"},
			failureDomain: "zone-b",
			want:          false,
		},
		{
			name:          "matching failure domain with nil selector matches",
			spec:          infrav1alpha1.TartHostSpec{FailureDomain: "zone-a"},
			failureDomain: "zone-a",
			want:          true,
		},
		{
			name:     "architecture mismatch is rejected",
			spec:     infrav1alpha1.TartHostSpec{Architecture: "amd64"},
			selector: &infrav1alpha1.HostSelector{Architecture: "arm64"},
			want:     false,
		},
		{
			name:       "label selector match",
			hostLabels: map[string]string{"pool": "gpu"},
			spec:       infrav1alpha1.TartHostSpec{},
			selector: &infrav1alpha1.HostSelector{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"pool": "gpu"}},
			},
			want: true,
		},
		{
			name:       "label selector mismatch",
			hostLabels: map[string]string{"pool": "cpu"},
			spec:       infrav1alpha1.TartHostSpec{},
			selector: &infrav1alpha1.HostSelector{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"pool": "gpu"}},
			},
			want: false,
		},
		{
			name: "invalid label selector expression is rejected rather than treated as match-all",
			spec: infrav1alpha1.TartHostSpec{},
			selector: &infrav1alpha1.HostSelector{
				Selector: metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "pool", Operator: "InvalidOperator"},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := MatchesForFailureDomain(tt.hostLabels, tt.spec, tt.selector, tt.failureDomain); got != tt.want {
				t.Errorf("MatchesForFailureDomain() = %v, want %v", got, tt.want)
			}
		})
	}
}
