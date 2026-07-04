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
		Spec: infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-a"},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Spec:       corev1.NodeSpec{ProviderID: "tart://host-b"},
		Status: corev1.NodeStatus{
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

	observation, observed, err := observer.Observe(context.Background(), machine)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if !observed {
		t.Fatal("Observe() observed = false, want true")
	}
	if observation.MachineProviderID != "tart://host-a" ||
		observation.NodeProviderID != "tart://host-b" ||
		!observation.NodeReady {
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

	_, observed, err := NewObserver(managementClient).Observe(context.Background(), machine)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observed {
		t.Fatal("Observe() observed = true, want false")
	}
}
