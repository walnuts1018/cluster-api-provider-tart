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

package bootreport

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

var testBootstrapPayloadDigest = "sha256:" + strings.Repeat("d", 64)

type staticPlanProvider struct {
	plan agentprotocol.SignedPlan
}

func (provider staticPlanProvider) GetPlan(
	context.Context,
	client.ObjectKey,
) (agentprotocol.SignedPlan, error) {
	return provider.plan, nil
}

func TestServiceStartsRollbackWhenBootReportShowsMountFailure(t *testing.T) {
	ctx := t.Context()
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
	if persisted.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack {
		t.Fatalf("phase after incomplete report = %q, want RollingBack", persisted.Status.Phase)
	}
	degraded := apimeta.FindStatusCondition(persisted.Status.Conditions, appupdate.ConditionDegraded)
	if degraded == nil || degraded.Reason != "BootFailed" {
		t.Fatalf("Degraded condition = %#v", degraded)
	}
	if persisted.Status.LastBootReport == nil || persisted.Status.LastBootReport.DataMounted {
		t.Fatalf("lastBootReport = %#v, want persisted incomplete report", persisted.Status.LastBootReport)
	}
}

func TestServiceRecordsCompletedBootAndKeepsDuplicateIdempotent(t *testing.T) {
	ctx := t.Context()
	service, k8sClient, key, planDigest := newTestService(t)
	firstTime := metav1.NewTime(time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC))
	complete := agentprotocol.BootReportRequest{
		APIVersion:             agentprotocol.APIVersion,
		OperationUID:           "operation-uid",
		PlanDigest:             planDigest,
		BootID:                 "boot-id",
		ActiveSlot:             "B",
		ArtifactGeneration:     2,
		StateMounted:           true,
		DataMounted:            true,
		BootstrapApplied:       true,
		BootstrapPayloadDigest: testBootstrapPayloadDigest,
	}
	if err := service.ReportBoot(ctx, key, complete, firstTime); err != nil {
		t.Fatalf("ReportBoot(complete) error = %v", err)
	}
	persisted := getOperation(t, ctx, k8sClient, key)
	if persisted.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth {
		t.Fatalf("phase after complete report = %q, want AwaitingHealth", persisted.Status.Phase)
	}
	if !persisted.Status.LastBootReport.ReportedAt.Equal(&firstTime) {
		t.Fatalf("reportedAt = %v, want %v", persisted.Status.LastBootReport.ReportedAt, firstTime)
	}
	if persisted.Status.LastBootReport.BootstrapPayloadDigest != testBootstrapPayloadDigest {
		t.Fatalf("bootstrapPayloadDigest = %q, want %q", persisted.Status.LastBootReport.BootstrapPayloadDigest, testBootstrapPayloadDigest)
	}

	duplicateTime := metav1.NewTime(firstTime.Add(time.Minute))
	if err := service.ReportBoot(ctx, key, complete, duplicateTime); err != nil {
		t.Fatalf("ReportBoot(duplicate) error = %v", err)
	}
	persisted = getOperation(t, ctx, k8sClient, key)
	if !persisted.Status.LastBootReport.ReportedAt.Equal(&firstTime) {
		t.Fatalf("duplicate changed reportedAt to %v", persisted.Status.LastBootReport.ReportedAt)
	}
}

func TestServiceCountsWrongSlotBootReportAsBootFailureAttempt(t *testing.T) {
	ctx := t.Context()
	service, k8sClient, key, planDigest := newTestService(t)

	for attempt := int32(1); attempt <= 3; attempt++ {
		report := agentprotocol.BootReportRequest{
			APIVersion:             agentprotocol.APIVersion,
			OperationUID:           "operation-uid",
			PlanDigest:             planDigest,
			BootID:                 "wrong-slot-boot-" + strconv.Itoa(int(attempt)),
			ActiveSlot:             "A",
			ArtifactGeneration:     1,
			StateMounted:           true,
			DataMounted:            true,
			BootstrapApplied:       true,
			BootstrapPayloadDigest: testBootstrapPayloadDigest,
		}
		if err := service.ReportBoot(ctx, key, report, metav1.Now()); err != nil {
			t.Fatalf("attempt %d ReportBoot() error = %v", attempt, err)
		}
		persisted := getOperation(t, ctx, k8sClient, key)
		if persisted.Status.Attempt != attempt {
			t.Fatalf("attempt %d persisted attempt = %d", attempt, persisted.Status.Attempt)
		}
		if attempt < 3 {
			if persisted.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseBootTrial {
				t.Fatalf("attempt %d phase = %q, want BootTrial", attempt, persisted.Status.Phase)
			}
			continue
		}
		if persisted.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack {
			t.Fatalf("attempt %d phase = %q, want RollingBack", attempt, persisted.Status.Phase)
		}
		degraded := apimeta.FindStatusCondition(persisted.Status.Conditions, appupdate.ConditionDegraded)
		if degraded == nil || degraded.Reason != "BootFailed" {
			t.Fatalf("attempt %d Degraded condition = %#v", attempt, degraded)
		}
	}
}

func TestServiceRejectsConflictingReportAfterBootCompletion(t *testing.T) {
	ctx := t.Context()
	service, _, key, planDigest := newTestService(t)
	report := agentprotocol.BootReportRequest{
		APIVersion:             agentprotocol.APIVersion,
		OperationUID:           "operation-uid",
		PlanDigest:             planDigest,
		BootID:                 "boot-id",
		ActiveSlot:             "B",
		ArtifactGeneration:     2,
		StateMounted:           true,
		DataMounted:            true,
		BootstrapApplied:       true,
		BootstrapPayloadDigest: testBootstrapPayloadDigest,
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

func TestServiceRecordsRollbackBootResult(t *testing.T) {
	tests := []struct {
		name      string
		report    agentprotocol.BootReportRequest
		wantPhase infrastructurev1beta1.TartHostOperationPhase
	}{
		{
			name: "旧slot健全",
			report: agentprotocol.BootReportRequest{
				APIVersion:             agentprotocol.APIVersion,
				OperationUID:           "operation-uid",
				BootID:                 "rollback-boot",
				ActiveSlot:             "A",
				ArtifactGeneration:     1,
				StateMounted:           true,
				DataMounted:            true,
				BootstrapApplied:       true,
				BootstrapPayloadDigest: testBootstrapPayloadDigest,
			},
			wantPhase: infrastructurev1beta1.TartHostOperationPhaseFailed,
		},
		{
			name: "旧slotも不健全",
			report: agentprotocol.BootReportRequest{
				APIVersion:         agentprotocol.APIVersion,
				OperationUID:       "operation-uid",
				BootID:             "rollback-boot",
				ActiveSlot:         "A",
				ArtifactGeneration: 1,
				StateMounted:       true,
			},
			wantPhase: infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			service, k8sClient, key, planDigest := newTestService(t)
			operation := getOperation(t, ctx, k8sClient, key)
			operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseRollingBack
			if err := k8sClient.Status().Update(ctx, operation); err != nil {
				t.Fatalf("update operation status: %v", err)
			}
			tt.report.PlanDigest = planDigest
			if err := service.ReportBoot(ctx, key, tt.report, metav1.Now()); err != nil {
				t.Fatalf("ReportBoot() error = %v", err)
			}
			persisted := getOperation(t, ctx, k8sClient, key)
			if persisted.Status.Phase != tt.wantPhase {
				t.Fatalf("phase = %q, want %q", persisted.Status.Phase, tt.wantPhase)
			}
		})
	}
}

func newTestService(
	t *testing.T,
) (*Service, client.Client, client.ObjectKey, string) {
	t.Helper()
	plan := agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-uid",
		HostUID:       "host-uid",
		OperationType: agentprotocol.OperationTypeUpdate,
		ActiveSlot:    "A",
		Deadline:      time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-disk",
			SerialNumber: "disk-serial",
			MinSizeBytes: 1,
		},
		Artifact: &agentprotocol.Artifact{
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
			Type:        infrastructurev1beta1.OperationTypeUpdate,
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
