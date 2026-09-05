// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
			name: "retainedFromのみではReusableにしない",
			spec: infrav1alpha1.TartHostSpec{
				RetainedFrom: &infrav1alpha1.RetainedFrom{UID: types.UID("prev")},
			},
			want: Retained,
		},
		{
			name: "reusePolicyがReusableでも承認がなければRetainedのまま",
			spec: infrav1alpha1.TartHostSpec{
				RetainedFrom: &infrav1alpha1.RetainedFrom{UID: types.UID("prev")},
				ReusePolicy:  infrav1alpha1.ReusePolicyReusable,
			},
			want: Retained,
		},
		{
			name: "承認のretainedFromUIDが古いretainedFromと一致しなければRetainedのまま",
			spec: infrav1alpha1.TartHostSpec{
				RetainedFrom:  &infrav1alpha1.RetainedFrom{UID: types.UID("prev-2")},
				ReusePolicy:   infrav1alpha1.ReusePolicyReusable,
				ReuseApproval: &infrav1alpha1.ReuseApproval{RetainedFromUID: types.UID("prev-1")},
				ReuseMode:     infrav1alpha1.ReuseModeAdopt,
			},
			want: Retained,
		},
		{
			name: "reusePolicy・一致する承認・reuseModeが揃えばReusable",
			spec: infrav1alpha1.TartHostSpec{
				RetainedFrom:  &infrav1alpha1.RetainedFrom{UID: types.UID("prev")},
				ReusePolicy:   infrav1alpha1.ReusePolicyReusable,
				ReuseApproval: &infrav1alpha1.ReuseApproval{RetainedFromUID: types.UID("prev")},
				ReuseMode:     infrav1alpha1.ReuseModeReprovision,
			},
			want: Reusable,
		},
		{
			name: "consumerRefが優先されretainedFromがあってもClaimed",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:  &corev1.ObjectReference{UID: types.UID("m1")},
				RetainedFrom: &infrav1alpha1.RetainedFrom{UID: types.UID("prev")},
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
