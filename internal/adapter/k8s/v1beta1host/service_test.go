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

package v1beta1host

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
)

func TestServiceUpdateCapabilitiesPersistsDiscoveredCapabilities(t *testing.T) {
	t.Parallel()

	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "host-a",
			Namespace:  "default",
			Generation: 2,
		},
	}
	k8sClient := newFakeClient(t, host)
	service := NewService(k8sClient)
	capabilities, err := capabilitydomain.NewSet(capabilitydomain.PowerOn, capabilitydomain.PowerOff)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	if err := service.UpdateCapabilities(t.Context(), host, capabilities); err != nil {
		t.Fatalf("UpdateCapabilities() error = %v", err)
	}

	current := &infrastructurev1beta1.TartHost{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(host), current); err != nil {
		t.Fatalf("get TartHost: %v", err)
	}
	if got := current.Status.Capabilities; len(got) != 2 ||
		got[0] != infrastructurev1beta1.CapabilityPowerOn ||
		got[1] != infrastructurev1beta1.CapabilityPowerOff {
		t.Fatalf("status.capabilities = %v, want [PowerOn PowerOff]", got)
	}
	if current.Status.ObservedGeneration != current.Generation {
		t.Fatalf("observedGeneration = %d, want %d", current.Status.ObservedGeneration, current.Generation)
	}
}

func TestServiceUpdateCapabilitiesNoopsWhenCapabilitiesMatch(t *testing.T) {
	t.Parallel()

	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "host-a",
			Namespace:  "default",
			Generation: 3,
		},
		Status: infrastructurev1beta1.TartHostStatus{
			Capabilities: []infrastructurev1beta1.Capability{
				infrastructurev1beta1.CapabilityPowerOn,
			},
		},
	}
	k8sClient := newFakeClient(t, host)
	service := NewService(k8sClient)
	capabilities, err := capabilitydomain.NewSet(capabilitydomain.PowerOn)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if err := service.UpdateCapabilities(t.Context(), host, capabilities); err != nil {
		t.Fatalf("UpdateCapabilities() error = %v", err)
	}
}

func TestServiceUpdatePowerStatePersistsObservedState(t *testing.T) {
	t.Parallel()

	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "host-a",
			Namespace:  "default",
			Generation: 4,
		},
	}
	k8sClient := newFakeClient(t, host)
	service := NewService(k8sClient)

	if err := service.UpdatePowerState(t.Context(), host, infrastructurev1beta1.PowerStateOn); err != nil {
		t.Fatalf("UpdatePowerState() error = %v", err)
	}

	current := &infrastructurev1beta1.TartHost{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(host), current); err != nil {
		t.Fatalf("get TartHost: %v", err)
	}
	if current.Status.PowerState != infrastructurev1beta1.PowerStateOn {
		t.Fatalf("status.powerState = %q, want On", current.Status.PowerState)
	}
	if current.Status.ObservedGeneration != current.Generation {
		t.Fatalf("observedGeneration = %d, want %d", current.Status.ObservedGeneration, current.Generation)
	}
}

func TestServiceUpdateBootStatePersistsObservedState(t *testing.T) {
	t.Parallel()

	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "host-a",
			Namespace:  "default",
			Generation: 5,
		},
	}
	k8sClient := newFakeClient(t, host)
	service := NewService(k8sClient)

	state := infrastructurev1beta1.BootStateStatus{
		OverrideEnabled: true,
		OverrideTarget:  infrastructurev1beta1.BootTargetVirtualMedia,
		VirtualMedia: infrastructurev1beta1.VirtualMediaStatus{
			Inserted:    true,
			Image:       "https://controller.example.test/agent.iso",
			OperationID: "f4353748-c9ea-41c6-b321-94197b64330e",
		},
	}
	if err := service.UpdateBootState(t.Context(), host, state); err != nil {
		t.Fatalf("UpdateBootState() error = %v", err)
	}

	current := &infrastructurev1beta1.TartHost{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(host), current); err != nil {
		t.Fatalf("get TartHost: %v", err)
	}
	if current.Status.BootState == nil {
		t.Fatal("status.bootState = nil, want observed state")
	}
	if !current.Status.BootState.OverrideEnabled ||
		current.Status.BootState.OverrideTarget != infrastructurev1beta1.BootTargetVirtualMedia {
		t.Fatalf("status.bootState = %#v, want VirtualMedia override", current.Status.BootState)
	}
	if !current.Status.BootState.VirtualMedia.Inserted ||
		current.Status.BootState.VirtualMedia.Image == "" ||
		current.Status.BootState.VirtualMedia.OperationID == "" {
		t.Fatalf("status.bootState.virtualMedia = %#v, want mounted media", current.Status.BootState.VirtualMedia)
	}
	if current.Status.ObservedGeneration != current.Generation {
		t.Fatalf("observedGeneration = %d, want %d", current.Status.ObservedGeneration, current.Generation)
	}
}

func newFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHost{}).
		WithObjects(objects...).
		Build()
}
