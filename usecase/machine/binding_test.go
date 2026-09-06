package machine

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TestDeletionDrainCompleteRequiresInfrastructureDeletionStageは、CAPI Machineのdeletion reasonのうち
// drain/volume detach完了を示すreasonだけがHost解放を進めてよい根拠になることを確認する境界テストである。
func TestDeletionDrainCompleteRequiresInfrastructureDeletionStage(t *testing.T) {
	t.Parallel()

	deletionTime := metav1.Now()
	tests := []struct {
		name   string
		reason string
		want   bool
	}{
		{name: "not deleting", want: false},
		{name: "draining", reason: clusterv1.MachineDeletingDrainingNodeReason, want: false},
		{name: "waiting for infrastructure", reason: clusterv1.MachineDeletingWaitingForInfrastructureDeletionReason, want: true},
		{name: "waiting for bootstrap", reason: clusterv1.MachineDeletingWaitingForBootstrapDeletionReason, want: true},
		{name: "internal error", reason: clusterv1.MachineDeletingInternalErrorReason, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			machine := &clusterv1.Machine{}
			if test.reason != "" {
				machine.DeletionTimestamp = &deletionTime
				machine.Status.Conditions = []metav1.Condition{{
					Type:   clusterv1.MachineDeletingCondition,
					Status: metav1.ConditionTrue,
					Reason: test.reason,
				}}
			}
			if got := DeletionDrainComplete(machine); got != test.want {
				t.Errorf("DeletionDrainComplete() = %t, want %t", got, test.want)
			}
		})
	}
}
