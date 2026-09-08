package tartcontrolplane

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
)

func TestSetControlPlaneStatusProjectsReadinessAndVersionObservation(t *testing.T) {
	t.Parallel()

	allReadyMachines := []clusterv1.Machine{
		machineWithStatus("cp-0", true, true, true),
		machineWithStatus("cp-1", true, true, true),
	}
	tests := []struct {
		name                   string
		machines               []clusterv1.Machine
		desired                int32
		bootstrap              controlPlaneBootstrapState
		upgrade                controlPlaneKubernetesUpgradeState
		wantAvailable          metav1.ConditionStatus
		wantUpToDate           metav1.ConditionStatus
		wantKubernetesUpdating metav1.ConditionStatus
	}{
		{
			name:                   "all machines and version converged",
			machines:               allReadyMachines,
			desired:                2,
			bootstrap:              controlPlaneBootstrapState{initialized: true, workloadReady: true},
			upgrade:                controlPlaneKubernetesUpgradeState{observedVersion: "v1.34.0"},
			wantAvailable:          metav1.ConditionTrue,
			wantUpToDate:           metav1.ConditionTrue,
			wantKubernetesUpdating: metav1.ConditionFalse,
		},
		{
			name:                   "unknown version is not up to date",
			machines:               allReadyMachines,
			desired:                2,
			bootstrap:              controlPlaneBootstrapState{initialized: true, workloadReady: true},
			upgrade:                controlPlaneKubernetesUpgradeState{observedVersion: "v1.34.0", unknown: true},
			wantAvailable:          metav1.ConditionTrue,
			wantUpToDate:           metav1.ConditionFalse,
			wantKubernetesUpdating: metav1.ConditionUnknown,
		},
		{
			name:                   "partial machine readiness",
			machines:               []clusterv1.Machine{machineWithStatus("cp-0", true, true, true)},
			desired:                2,
			bootstrap:              controlPlaneBootstrapState{initialized: true, workloadReady: true},
			upgrade:                controlPlaneKubernetesUpgradeState{observedVersion: "v1.34.0"},
			wantAvailable:          metav1.ConditionFalse,
			wantUpToDate:           metav1.ConditionFalse,
			wantKubernetesUpdating: metav1.ConditionFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			controlPlane := &controlplanev1alpha1.TartControlPlane{}
			controlPlane.Name = "cp"
			controlPlane.Generation = 4
			controlPlane.Spec.Version = "v1.34.0"

			setControlPlaneStatus(controlPlane, "cluster-a", tt.desired, tt.machines, tt.bootstrap, controlPlaneCARotationState{}, tt.upgrade)

			if *controlPlane.Status.Replicas != int32(len(tt.machines)) || *controlPlane.Status.ReadyReplicas != int32(min(len(tt.machines), int(tt.desired))) {
				t.Fatalf("replica status = replicas:%d ready:%d, want replicas:%d ready:%d", *controlPlane.Status.Replicas, *controlPlane.Status.ReadyReplicas, len(tt.machines), min(len(tt.machines), int(tt.desired)))
			}
			for conditionType, want := range map[string]metav1.ConditionStatus{
				controlplanev1alpha1.TartControlPlaneAvailableCondition:           tt.wantAvailable,
				controlplanev1alpha1.TartControlPlaneUpToDateCondition:            tt.wantUpToDate,
				controlplanev1alpha1.TartControlPlaneKubernetesUpgradingCondition: tt.wantKubernetesUpdating,
			} {
				condition := meta.FindStatusCondition(controlPlane.Status.Conditions, conditionType)
				if condition == nil || condition.Status != want {
					t.Fatalf("condition %s = %#v, want status %s", conditionType, condition, want)
				}
			}
		})
	}
}

func machineWithStatus(name string, ready, available, upToDate bool) clusterv1.Machine {
	machine := clusterv1.Machine{Name: name}
	for _, condition := range []struct {
		typeName string
		value    bool
	}{
		{typeName: clusterv1.MachineReadyCondition, value: ready},
		{typeName: clusterv1.MachineAvailableCondition, value: available},
		{typeName: clusterv1.MachineUpToDateCondition, value: upToDate},
	} {
		status := metav1.ConditionFalse
		if condition.value {
			status = metav1.ConditionTrue
		}
		machine.Status.Conditions = append(machine.Status.Conditions, metav1.Condition{Type: condition.typeName, Status: status})
	}
	return machine
}
