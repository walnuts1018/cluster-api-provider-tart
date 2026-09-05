package host

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec infrav1alpha1.TartHostSpec
		want Eligibility
	}{
		{
			name: "freshなHostはAvailable",
			spec: infrav1alpha1.TartHostSpec{},
			want: Available,
		},
		{
			name: "consumerRefがあればClaimed",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef: &corev1.ObjectReference{UID: types.UID("m1")},
			},
			want: Claimed,
		},
		{
			name: "previousConsumerRefのみではReusableにしない",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
			},
			want: Retained,
		},
		{
			name: "reusePolicyがAllowReuseでも承認がなければRetainedのまま",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
			},
			want: Retained,
		},
		{
			name: "previousConsumerRefと承認のUIDが空でもReusableにしない",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{},
				ReuseMode:           infrav1alpha1.ReuseModeAdopt,
			},
			want: Retained,
		},
		{
			name: "承認のUIDが空ならReusableにしない",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{},
				ReuseMode:           infrav1alpha1.ReuseModeAdopt,
			},
			want: Retained,
		},
		{
			name: "承認のpreviousConsumerUIDが古いpreviousConsumerRefと一致しなければRetainedのまま",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev-2")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{PreviousConsumerUID: types.UID("prev-1")},
				ReuseMode:           infrav1alpha1.ReuseModeAdopt,
			},
			want: Retained,
		},
		{
			name: "reusePolicy・一致する承認・reuseModeが揃えばReusable",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{PreviousConsumerUID: types.UID("prev")},
				ReuseMode:           infrav1alpha1.ReuseModeReprovision,
			},
			want: Reusable,
		},
		{
			name: "consumerRefが優先されpreviousConsumerRefがあってもClaimed",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:         &corev1.ObjectReference{UID: types.UID("m1")},
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("prev")},
			},
			want: Claimed,
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
