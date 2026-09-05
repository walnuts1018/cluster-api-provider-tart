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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
			name: "claimed host with empty consumer UID cannot be forgotten",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:    &corev1.ObjectReference{},
				ForgetApproval: &infrav1alpha1.ForgetApproval{},
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
			name: "retained host with empty retained UID cannot be forgotten",
			spec: infrav1alpha1.TartHostSpec{
				RetainedFrom:   &infrav1alpha1.RetainedFrom{},
				ForgetApproval: &infrav1alpha1.ForgetApproval{},
			},
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

func TestTartHostReconcilerReportsIdentityConflictForEveryRelatedHost(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	hosts := make([]*infrav1alpha1.TartHost, 3)
	for index := range hosts {
		hosts[index] = &infrav1alpha1.TartHost{
			Name: fmt.Sprintf("host-%d", index),
			Spec: infrav1alpha1.TartHostSpec{
				ID:         fmt.Sprintf("018f3c5e-5f8a-7c1b-9a2d-123456789ab%d", index),
				MACAddress: "02:00:00:00:00:01",
			},
		}
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1alpha1.TartHost{}).WithObjects(hosts[0], hosts[1], hosts[2]).Build()
	reconciler := &TartHostReconciler{Client: fakeClient}

	for range 2 {
		if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: hosts[0].Name}); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
	}

	for index := range hosts {
		observed := &infrav1alpha1.TartHost{}
		if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: hosts[index].Name}, observed); err != nil {
			t.Fatalf("Get(TartHost) error = %v", err)
		}
		condition := meta.FindStatusCondition(observed.Status.Conditions, infrav1alpha1.TartHostReadyCondition)
		if condition == nil || condition.Reason != infrav1alpha1.ReasonIdentityConflict {
			t.Errorf("%s Ready condition = %#v, want IdentityConflict", observed.Name, condition)
		}
	}
}
