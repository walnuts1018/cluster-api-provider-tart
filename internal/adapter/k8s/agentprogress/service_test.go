package agentprogress

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentprogressdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentprogress"
)

func TestServiceAppliesSequenceOneTwoTwoOneFourThree(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKey{Namespace: "default", Name: "operation"}
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			PlanDigest:  "sha256:" + strings.Repeat("a", 64),
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host",
				UID:       types.UID("host-uid"),
			},
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent,
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	service := NewService(k8sClient)

	sequences := []int64{1, 2, 2, 1, 4, 3}
	want := []agentprogressdomain.Decision{
		agentprogressdomain.DecisionApply,
		agentprogressdomain.DecisionApply,
		agentprogressdomain.DecisionDuplicate,
		agentprogressdomain.DecisionDuplicate,
		agentprogressdomain.DecisionGap,
		agentprogressdomain.DecisionApply,
	}
	for index, sequence := range sequences {
		result, err := service.Report(
			ctx,
			key,
			"operation-uid",
			operation.Spec.PlanDigest,
			sequence,
			agentprogressdomain.Progress{
				Step:      "WriteImage",
				DiskRole:  "OS-A",
				Percent:   100,
				Completed: sequence == 3,
			},
		)
		if err != nil {
			t.Fatalf("Report(sequence=%d) error = %v", sequence, err)
		}
		if result.Decision != want[index] {
			t.Fatalf("Report(sequence=%d) decision = %q, want %q", sequence, result.Decision, want[index])
		}
	}

	persisted := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(ctx, key, persisted); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if persisted.Status.AgentSequence != 3 {
		t.Fatalf("AgentSequence = %d, want 3", persisted.Status.AgentSequence)
	}
	if len(persisted.Status.CompletedSteps) != 1 || persisted.Status.CompletedSteps[0] != "WriteImage" {
		t.Fatalf("CompletedSteps = %#v, want WriteImage", persisted.Status.CompletedSteps)
	}
	if persisted.Status.AgentProgress == nil ||
		persisted.Status.AgentProgress.Step != "WriteImage" ||
		persisted.Status.AgentProgress.DiskRole != "OS-A" ||
		persisted.Status.AgentProgress.Percent != 100 {
		t.Fatalf("AgentProgress = %#v", persisted.Status.AgentProgress)
	}
	if persisted.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseVerifying {
		t.Fatalf("Phase = %q, want Verifying", persisted.Status.Phase)
	}
}
