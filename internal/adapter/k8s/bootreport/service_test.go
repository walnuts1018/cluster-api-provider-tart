package bootreport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type staticPlanProvider struct {
	plan agentprotocol.SignedPlan
}

func (provider staticPlanProvider) GetPlan(
	context.Context,
	client.ObjectKey,
) (agentprotocol.SignedPlan, error) {
	return provider.plan, nil
}

func TestServiceRecordsIncompleteBootAndAdvancesWhenComplete(t *testing.T) {
	ctx := context.Background()
	service, k8sClient, key, planDigest := newTestService(t)
	firstTime := metav1.NewTime(time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC))
	incomplete := agentprotocol.BootReportRequest{
		APIVersion:         agentprotocol.APIVersion,
		OperationUID:       "operation-uid",
		PlanDigest:         planDigest,
		BootID:             "boot-id",
		ActiveSlot:         "B",
		ArtifactGeneration: 2,
		StateMounted:       true,
	}
	if err := service.ReportBoot(ctx, key, incomplete, firstTime); err != nil {
		t.Fatalf("ReportBoot(incomplete) error = %v", err)
	}
	persisted := getOperation(t, ctx, k8sClient, key)
	if persisted.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseBootTrial {
		t.Fatalf("phase after incomplete report = %q, want BootTrial", persisted.Status.Phase)
	}
	if persisted.Status.LastBootReport == nil || persisted.Status.LastBootReport.DataMounted {
		t.Fatalf("lastBootReport = %#v, want persisted incomplete report", persisted.Status.LastBootReport)
	}

	complete := incomplete
	complete.DataMounted = true
	complete.BootstrapApplied = true
	secondTime := metav1.NewTime(firstTime.Add(time.Minute))
	if err := service.ReportBoot(ctx, key, complete, secondTime); err != nil {
		t.Fatalf("ReportBoot(complete) error = %v", err)
	}
	persisted = getOperation(t, ctx, k8sClient, key)
	if persisted.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth {
		t.Fatalf("phase after complete report = %q, want AwaitingHealth", persisted.Status.Phase)
	}
	if !persisted.Status.LastBootReport.ReportedAt.Equal(&secondTime) {
		t.Fatalf("reportedAt = %v, want %v", persisted.Status.LastBootReport.ReportedAt, secondTime)
	}

	duplicateTime := metav1.NewTime(secondTime.Add(time.Minute))
	if err := service.ReportBoot(ctx, key, complete, duplicateTime); err != nil {
		t.Fatalf("ReportBoot(duplicate) error = %v", err)
	}
	persisted = getOperation(t, ctx, k8sClient, key)
	if !persisted.Status.LastBootReport.ReportedAt.Equal(&secondTime) {
		t.Fatalf("duplicate changed reportedAt to %v", persisted.Status.LastBootReport.ReportedAt)
	}
}

func TestServiceRejectsConflictingReportAfterBootCompletion(t *testing.T) {
	ctx := context.Background()
	service, _, key, planDigest := newTestService(t)
	report := agentprotocol.BootReportRequest{
		APIVersion:         agentprotocol.APIVersion,
		OperationUID:       "operation-uid",
		PlanDigest:         planDigest,
		BootID:             "boot-id",
		ActiveSlot:         "B",
		ArtifactGeneration: 2,
		StateMounted:       true,
		DataMounted:        true,
		BootstrapApplied:   true,
	}
	now := metav1.Now()
	if err := service.ReportBoot(ctx, key, report, now); err != nil {
		t.Fatalf("ReportBoot() error = %v", err)
	}
	report.BootID = "different-boot"
	if err := service.ReportBoot(ctx, key, report, now); !errors.Is(err, ErrReportConflict) {
		t.Fatalf("ReportBoot(conflict) error = %v, want ErrReportConflict", err)
	}
}

func newTestService(
	t *testing.T,
) (*Service, client.Client, client.ObjectKey, string) {
	t.Helper()
	plan := agentprotocol.Plan{
		APIVersion:   agentprotocol.APIVersion,
		OperationUID: "operation-uid",
		HostUID:      "host-uid",
		Deadline:     time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
		RootDevice: agentprotocol.RootDevice{
			SerialNumber: "disk-serial",
			MinSizeBytes: 1,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/os@sha256:" + strings.Repeat("b", 64),
			ManifestDigest: "sha256:" + strings.Repeat("c", 64),
			Generation:     2,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityB,
		},
		Steps: []agentprotocol.PlanStep{{Name: "BootTrial"}},
	}
	validated, err := agentprotocol.ValidatePlan(plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	digest, err := validated.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	key := client.ObjectKey{Namespace: "default", Name: "operation"}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			PlanDigest:  digest.String(),
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host",
				UID:       types.UID("host-uid"),
			},
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseBootTrial,
		},
	}
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	provider := staticPlanProvider{plan: agentprotocol.SignedPlan{Plan: plan}}
	return NewService(k8sClient, provider), k8sClient, key, digest.String()
}

func getOperation(
	t *testing.T,
	ctx context.Context,
	k8sClient client.Client,
	key client.ObjectKey,
) *infrastructurev1beta1.TartHostOperation {
	t.Helper()
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(ctx, key, operation); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	return operation
}
