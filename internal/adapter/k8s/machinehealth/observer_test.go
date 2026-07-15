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

package machinehealth

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestObserverReadsWorkloadNode(t *testing.T) {
	t.Parallel()

	testScheme := runtime.NewScheme()
	for name, addToScheme := range map[string]func(*runtime.Scheme) error{
		"core":       corev1.AddToScheme,
		"clusterAPI": clusterv1.AddToScheme,
		"tart":       infrastructurev1beta1.AddToScheme,
	} {
		if err := addToScheme(testScheme); err != nil {
			t.Fatalf("%s AddToScheme() error = %v", name, err)
		}
	}

	coreMachine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default"},
		Spec: clusterv1.MachineSpec{
			ClusterName: "cluster-a",
		},
		Status: clusterv1.MachineStatus{
			NodeRef: clusterv1.MachineNodeReference{Name: "node-a"},
		},
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
				Name:       coreMachine.Name,
			}},
		},
		Spec:   infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-a"},
		Status: infrastructurev1beta1.TartMachineStatus{InstalledMachineID: "machine-id-a"},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Spec:       corev1.NodeSpec{ProviderID: "tart://host-b"},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				MachineID: "machine-id-b",
			},
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	managementClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(coreMachine).
		Build()
	workloadClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(node).
		Build()
	observer := NewObserver(managementClient)
	observer.newWorkloadClient = func(
		_ context.Context,
		source string,
		_ client.Client,
		cluster client.ObjectKey,
	) (client.Client, error) {
		if source != remoteClientSource {
			t.Fatalf("source = %q, want %q", source, remoteClientSource)
		}
		if cluster != (client.ObjectKey{Namespace: "default", Name: "cluster-a"}) {
			t.Fatalf("cluster = %s", cluster)
		}
		return workloadClient, nil
	}

	observation, observed, err := observer.Observe(t.Context(), machine)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if !observed {
		t.Fatal("Observe() observed = false, want true")
	}
	if observation.MachineProviderID != "tart://host-a" ||
		observation.NodeProviderID != "tart://host-b" ||
		!observation.NodeReady ||
		observation.ExpectedMachineID != "machine-id-a" ||
		observation.ObservedMachineID != "machine-id-b" {
		t.Fatalf("Observe() = %#v", observation)
	}
}

func TestObserverSkipsMachineWithoutNodeReference(t *testing.T) {
	t.Parallel()

	testScheme := runtime.NewScheme()
	if err := clusterv1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	coreMachine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default"},
		Spec:       clusterv1.MachineSpec{ClusterName: "cluster-a"},
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
				Name:       coreMachine.Name,
				UID:        types.UID("machine-a-uid"),
			}},
		},
	}
	managementClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(coreMachine).
		Build()

	_, observed, err := NewObserver(managementClient).Observe(t.Context(), machine)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observed {
		t.Fatal("Observe() observed = true, want false")
	}
}
