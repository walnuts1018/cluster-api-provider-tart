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

package distributionlifecycle

import (
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

func TestStatusStoreは完了StepとSnapshotRefをStatusへ保存する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	operation := testOperation()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	store := NewStatusStore(k8sClient)
	plan := controlPlanePlan(t)
	snapshotRef := &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      "etcd-snapshot-1",
		UID:       types.UID("snapshot-uid"),
	}

	if err := store.RecordStep(t.Context(), operation, plan, domain.StepPreflightCompleted, nil); err != nil {
		t.Fatalf("RecordStep(PreflightCompleted) error = %v", err)
	}
	if err := store.RecordStep(t.Context(), operation, plan, domain.StepSnapshotCreated, snapshotRef); err != nil {
		t.Fatalf("RecordStep(SnapshotCreated) error = %v", err)
	}

	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	wantSteps := []string{"PreflightCompleted", "SnapshotCreated"}
	if !equalStrings(current.Status.CompletedSteps, wantSteps) {
		t.Fatalf("completedSteps = %#v, want %#v", current.Status.CompletedSteps, wantSteps)
	}
	if current.Status.SnapshotRef == nil || current.Status.SnapshotRef.Name != "etcd-snapshot-1" {
		t.Fatalf("snapshotRef = %#v, want etcd-snapshot-1", current.Status.SnapshotRef)
	}
	if current.Status.LifecyclePhase != string(LifecyclePhaseSnapshot) {
		t.Fatalf("lifecyclePhase = %q, want Snapshot", current.Status.LifecyclePhase)
	}
}

func TestStatusStoreは重複Stepを再保存しない(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	operation := testOperation()
	operation.Status.CompletedSteps = []string{"PreflightCompleted"}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	store := NewStatusStore(k8sClient)

	if err := store.RecordStep(t.Context(), operation, workerPlan(t), domain.StepPreflightCompleted, nil); err != nil {
		t.Fatalf("RecordStep(duplicate) error = %v", err)
	}

	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if !equalStrings(current.Status.CompletedSteps, []string{"PreflightCompleted"}) {
		t.Fatalf("completedSteps = %#v, want duplicate suppressed", current.Status.CompletedSteps)
	}
}

func TestStatusStoreはSnapshotRefなしのSnapshotCreatedを拒否する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	operation := testOperation()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	store := NewStatusStore(k8sClient)
	plan := controlPlanePlan(t)
	if err := store.RecordStep(t.Context(), operation, plan, domain.StepPreflightCompleted, nil); err != nil {
		t.Fatalf("RecordStep(PreflightCompleted) error = %v", err)
	}

	if err := store.RecordStep(t.Context(), operation, plan, domain.StepSnapshotCreated, nil); err == nil {
		t.Fatal("RecordStep(SnapshotCreated) error = nil, want SnapshotRef required")
	}
}

func TestStatusStoreは永続化済みSnapshotRefでKubeadmAppliedを記録する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	operation := testOperation()
	operation.Status.CompletedSteps = []string{"PreflightCompleted", "SnapshotCreated", "TargetSlotWritten"}
	operation.Status.SnapshotRef = &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      "etcd-snapshot-1",
		UID:       types.UID("snapshot-uid"),
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	store := NewStatusStore(k8sClient)

	if err := store.RecordStep(t.Context(), operation, controlPlanePlan(t), domain.StepKubeadmApplied, nil); err != nil {
		t.Fatalf("RecordStep(KubeadmApplied) error = %v", err)
	}

	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	want := []string{"PreflightCompleted", "SnapshotCreated", "TargetSlotWritten", "KubeadmApplied"}
	if !equalStrings(current.Status.CompletedSteps, want) {
		t.Fatalf("completedSteps = %#v, want %#v", current.Status.CompletedSteps, want)
	}
}

func TestStatusStoreはStateMigration失敗時にRecoveryRequiredへ遷移しSnapshotRefを保持する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	operation := testOperation()
	operation.Spec.UpdateClass = infrastructurev1beta1.UpdateClassStateMigration
	operation.Status.SnapshotRef = &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      "etcd-snapshot-1",
		UID:       types.UID("snapshot-uid"),
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	store := NewStatusStore(k8sClient)

	if err := store.MarkRecoveryRequired(t.Context(), operation); err != nil {
		t.Fatalf("MarkRecoveryRequired() error = %v", err)
	}

	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired {
		t.Fatalf("phase = %q, want RecoveryRequired", current.Status.Phase)
	}
	if current.Status.SnapshotRef == nil || current.Status.SnapshotRef.Name != "etcd-snapshot-1" {
		t.Fatalf("snapshotRef = %#v, want retained", current.Status.SnapshotRef)
	}
}

func TestStatusStoreは各永続化Stepの直後に再実行されても重複記録しない(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	operation := testOperation()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(operation).
		Build()
	plan := controlPlanePlan(t)
	snapshotRef := &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      "etcd-snapshot-1",
		UID:       types.UID("snapshot-uid"),
	}
	steps := []domain.Step{
		domain.StepPreflightCompleted,
		domain.StepSnapshotCreated,
		domain.StepTargetSlotWritten,
		domain.StepKubeadmApplied,
		domain.StepTargetSlotBooted,
		domain.StepHealthVerified,
		domain.StepCommitted,
	}
	wantCompleted := make([]string, 0, len(steps))

	for index, step := range steps {
		store := NewStatusStore(k8sClient)
		stepSnapshotRef := (*infrastructurev1beta1.ResourceReference)(nil)
		if step == domain.StepSnapshotCreated {
			stepSnapshotRef = snapshotRef
		}
		current := getOperation(t, k8sClient, operation)

		if err := store.RecordStep(t.Context(), current, plan, step, stepSnapshotRef); err != nil {
			t.Fatalf("RecordStep(%q) error = %v", step, err)
		}

		wantCompleted = append(wantCompleted, string(step))
		assertRecordedStepState(t, k8sClient, operation, wantCompleted, step, snapshotRef)

		store = NewStatusStore(k8sClient)
		current = getOperation(t, k8sClient, operation)
		if err := store.RecordStep(t.Context(), current, plan, step, stepSnapshotRef); err != nil {
			t.Fatalf("RecordStep(%q duplicate) error = %v", step, err)
		}
		assertRecordedStepState(t, k8sClient, operation, wantCompleted, step, snapshotRef)

		if got := len(getOperation(t, k8sClient, operation).Status.CompletedSteps); got != index+1 {
			t.Fatalf("completedSteps length = %d, want %d after duplicate %q", got, index+1, step)
		}
	}
}

func testOperation() *infrastructurev1beta1.TartHostOperation {
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-host-a",
			Namespace: "default",
			UID:       types.UID("operation-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "0197d640-8d00-7a65-b67f-3f7c42a6935f",
			Type:        infrastructurev1beta1.OperationTypeUpdate,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host-a",
				UID:       types.UID("host-a"),
			},
			PlanDigest:           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			DesiredObjectsDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			TargetSlot:           infrastructurev1beta1.OSSlotB,
			UpdateClass:          infrastructurev1beta1.UpdateClassKubernetesBinary,
			Deadline:             metav1.NewTime(time.Now().Add(time.Hour)),
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		},
	}
}

func controlPlanePlan(t *testing.T) domain.Plan {
	t.Helper()
	plan, err := domain.BuildPlan(domain.PlanInput{
		OperationID:    "0197d640-8d00-7a65-b67f-3f7c42a6935f",
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    domain.UpdateClassKubernetesBinary,
		NodeRole:       domain.NodeRoleControlPlane,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	return plan
}

func workerPlan(t *testing.T) domain.Plan {
	t.Helper()
	plan, err := domain.BuildPlan(domain.PlanInput{
		OperationID:    "0197d640-8d00-7a65-b67f-3f7c42a6935f",
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    domain.UpdateClassKubernetesBinary,
		NodeRole:       domain.NodeRoleWorker,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	return plan
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func getOperation(
	t *testing.T,
	k8sClient client.Client,
	operation *infrastructurev1beta1.TartHostOperation,
) *infrastructurev1beta1.TartHostOperation {
	t.Helper()
	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	return current
}

func assertRecordedStepState(
	t *testing.T,
	k8sClient client.Client,
	operation *infrastructurev1beta1.TartHostOperation,
	wantCompleted []string,
	step domain.Step,
	snapshotRef *infrastructurev1beta1.ResourceReference,
) {
	t.Helper()
	current := getOperation(t, k8sClient, operation)
	if !equalStrings(current.Status.CompletedSteps, wantCompleted) {
		t.Fatalf("completedSteps = %#v, want %#v", current.Status.CompletedSteps, wantCompleted)
	}
	if step == domain.StepSnapshotCreated || slices.Contains(wantCompleted, string(domain.StepSnapshotCreated)) {
		if current.Status.SnapshotRef == nil || current.Status.SnapshotRef.Name != snapshotRef.Name {
			t.Fatalf("snapshotRef = %#v, want retained snapshot", current.Status.SnapshotRef)
		}
	}
	if current.Status.LifecyclePhase != string(phaseForStep(step)) {
		t.Fatalf("lifecyclePhase = %q, want %q", current.Status.LifecyclePhase, phaseForStep(step))
	}
	wantPhase := infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating
	if step == domain.StepCommitted {
		wantPhase = infrastructurev1beta1.TartHostOperationPhaseSucceeded
	}
	if current.Status.Phase != wantPhase {
		t.Fatalf("phase = %q, want %q", current.Status.Phase, wantPhase)
	}
}
