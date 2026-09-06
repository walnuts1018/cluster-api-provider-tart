package host

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec infrav1alpha1.TartHostSpec
		want hostdomain.Eligibility
	}{
		{
			name: "freshなHostはhostdomain.Available",
			spec: infrav1alpha1.TartHostSpec{},
			want: hostdomain.Available,
		},
		{
			name: "consumerRefがあればhostdomain.Claimed",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef: &corev1.ObjectReference{UID: types.UID("m1")},
			},
			want: hostdomain.Claimed,
		},
		{
			name: "previousConsumerRefのみではReusableにしない",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
			},
			want: hostdomain.Retained,
		},
		{
			name: "reusePolicyがAllowReuseでも承認がなければRetainedのまま",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
			},
			want: hostdomain.Retained,
		},
		{
			name: "previousConsumerRefと承認のUIDが空でもReusableにしない",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{},
				ReuseMode:           infrav1alpha1.ReuseModeAdopt,
			},
			want: hostdomain.Retained,
		},
		{
			name: "承認のUIDが空ならReusableにしない",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{},
				ReuseMode:           infrav1alpha1.ReuseModeAdopt,
			},
			want: hostdomain.Retained,
		},
		{
			name: "承認のpreviousConsumerUIDが古いpreviousConsumerRefと一致しなければRetainedのまま",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev-2")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{PreviousConsumerUID: types.UID("prev-1")},
				ReuseMode:           infrav1alpha1.ReuseModeAdopt,
			},
			want: hostdomain.Retained,
		},
		{
			name: "reusePolicy・一致する承認・reuseModeが揃えばhostdomain.Reusable",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{PreviousConsumerUID: types.UID("prev")},
				ReuseMode:           infrav1alpha1.ReuseModeReprovision,
			},
			want: hostdomain.Reusable,
		},
		{
			name: "consumerRefが優先されpreviousConsumerRefがあってもhostdomain.Claimed",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:         &corev1.ObjectReference{UID: types.UID("m1")},
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
			},
			want: hostdomain.Claimed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tt.spec); got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}
