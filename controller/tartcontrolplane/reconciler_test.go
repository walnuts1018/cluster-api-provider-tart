package tartcontrolplane

import (
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func TestDesiredControlPlaneReplicasRejectsZero(t *testing.T) {
	t.Parallel()

	zero := int32(0)
	cp := &controlplanev1alpha1.TartControlPlane{}
	cp.Spec.Replicas = &zero
	replicas, err := desiredControlPlaneReplicas(cp)
	if err == nil {
		t.Fatal("desiredControlPlaneReplicas() error = nil, want invalid replica count")
	}
	if replicas != 0 {
		t.Errorf("desiredControlPlaneReplicas() = %d, want 0 on error", replicas)
	}
}

func TestValidateControlPlaneMachineOwnersRejectsLabeledMachineFromAnotherOwner(t *testing.T) {
	t.Parallel()

	cp := &controlplanev1alpha1.TartControlPlane{}
	cp.Name = "control-plane"
	cp.UID = types.UID("control-plane-uid")
	isController := true
	machines := []clusterv1.Machine{{
		Name: "control-plane-1",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: controlplanev1alpha1.GroupVersion.String(),
			Kind:       controller.TartControlPlaneKind,
			Name:       cp.Name,
			UID:        types.UID("another-control-plane-uid"),
			Controller: &isController,
		}},
	}}
	if err := validateControlPlaneMachineOwners(machines, cp); err == nil {
		t.Fatal("validateControlPlaneMachineOwners() error = nil, want ownership mismatch")
	}
}

func TestMachineDeletionSpecClonesControlPlaneTemplateValues(t *testing.T) {
	t.Parallel()

	drainTimeout := int32(30)
	spec := controlplanev1alpha1.TartControlPlaneMachineTemplateDeletionSpec{NodeDrainTimeoutSeconds: &drainTimeout}
	deletion := machineDeletionSpec(spec)
	if deletion.NodeDrainTimeoutSeconds == nil || *deletion.NodeDrainTimeoutSeconds != drainTimeout {
		t.Fatalf("machineDeletionSpec() = %#v, want drain timeout %d", deletion, drainTimeout)
	}
	drainTimeout = 60
	if *deletion.NodeDrainTimeoutSeconds != 30 {
		t.Fatalf("machineDeletionSpec() reused source pointer, got %d", *deletion.NodeDrainTimeoutSeconds)
	}
}

func TestControlPlaneFailureDomainRoundRobinSkipsNonControlPlaneDomains(t *testing.T) {
	t.Parallel()

	controlPlane := true
	notControlPlane := false
	domains := []clusterv1.FailureDomain{
		{Name: "zone-b", ControlPlane: &controlPlane},
		{Name: "zone-worker", ControlPlane: &notControlPlane},
		{Name: "zone-a", ControlPlane: &controlPlane},
		{Name: "zone-a", ControlPlane: &controlPlane},
	}

	for ordinal, want := range []string{"zone-a", "zone-b", "zone-a"} {
		got, ok := controlPlaneFailureDomain(domains, ordinal)
		if !ok || got != want {
			t.Fatalf("controlPlaneFailureDomain(%d) = %q, %t; want %q, true", ordinal, got, ok, want)
		}
	}
	if _, ok := controlPlaneFailureDomain([]clusterv1.FailureDomain{{Name: "zone-worker", ControlPlane: &notControlPlane}}, 0); ok {
		t.Fatal("controlPlaneFailureDomain() returned a non-control-plane Failure Domain")
	}
}

func TestMachineWaitingForPreTerminateHookRequiresCAPIDeletionStage(t *testing.T) {
	t.Parallel()

	deletionTime := metav1.Now()
	base := clusterv1.Machine{DeletionTimestamp: &deletionTime}
	if machineWaitingForPreTerminateHook(&base) {
		t.Fatal("machineWaitingForPreTerminateHook() = true without CAPI deletion condition")
	}
	base.Status.Conditions = []metav1.Condition{{
		Type:   clusterv1.MachineDeletingCondition,
		Status: metav1.ConditionTrue,
		Reason: clusterv1.MachineDeletingDrainingNodeReason,
	}}
	if machineWaitingForPreTerminateHook(&base) {
		t.Fatal("machineWaitingForPreTerminateHook() = true while CAPI is still draining")
	}
	base.Status.Conditions[0].Reason = clusterv1.MachineDeletingWaitingForPreTerminateHookReason
	if !machineWaitingForPreTerminateHook(&base) {
		t.Fatal("machineWaitingForPreTerminateHook() = false at CAPI pre-terminate hook stage")
	}
}

func TestEtcdStatusHealthyRequiresLeaderAndNoErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status talos.EtcdStatus
		want   bool
	}{
		{name: "healthy", status: talos.EtcdStatus{MemberID: 1, Leader: 1}, want: true},
		{name: "missing member", status: talos.EtcdStatus{Leader: 1}},
		{name: "missing leader", status: talos.EtcdStatus{MemberID: 1}},
		{name: "reported error", status: talos.EtcdStatus{MemberID: 1, Leader: 1, Errors: []string{"unhealthy"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := etcdStatusHealthy(test.status); got != test.want {
				t.Errorf("etcdStatusHealthy(%#v) = %t, want %t", test.status, got, test.want)
			}
		})
	}
}
