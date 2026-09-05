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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestForgetApproved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec infrav1alpha1.TartHostSpec
		want bool
	}{
		{name: "fresh host does not need approval", want: true},
		{
			name: "claimed host needs matching consumer approval",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:    &corev1.ObjectReference{UID: types.UID("consumer")},
				ForgetApproval: &infrav1alpha1.ForgetApproval{ConsumerUID: types.UID("other")},
			},
		},
		{
			name: "retained host needs matching retained approval",
			spec: infrav1alpha1.TartHostSpec{
				RetainedFrom:   &infrav1alpha1.RetainedFrom{UID: types.UID("previous")},
				ForgetApproval: &infrav1alpha1.ForgetApproval{RetainedFromUID: types.UID("previous")},
			},
			want: true,
		},
		{
			name: "both bindings require both approvals",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:  &corev1.ObjectReference{UID: types.UID("consumer")},
				RetainedFrom: &infrav1alpha1.RetainedFrom{UID: types.UID("previous")},
				ForgetApproval: &infrav1alpha1.ForgetApproval{
					ConsumerUID:     types.UID("consumer"),
					RetainedFromUID: types.UID("previous"),
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := forgetApproved(tt.spec); got != tt.want {
				t.Errorf("forgetApproved() = %t, want %t", got, tt.want)
			}
		})
	}
}
