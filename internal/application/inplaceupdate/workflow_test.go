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

package inplaceupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	nodelifecycleapp "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	distributiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const targetKubernetesVersion = "v1.35.0"

func TestWorkflowはOperation作成後に一致する署名済みPlanを保存する(t *testing.T) {
	input := workflowInput(t)
	starter := &workflowOperationStarter{}
	writer := &recordingPlanWriter{}
	workflow := NewWorkflow(starter, writer, testPlanSigner(t))

	result, err := workflow.Start(t.Context(), input)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	started := result.Operation

	if started.UID == "" {
		t.Fatal("started Operation UID is empty")
	}
	if writer.operation == nil || writer.operation.UID != started.UID {
		t.Fatalf("written Operation UID = %v, want %q", writer.operation, started.UID)
	}
	if writer.plan.Value().OperationUID != started.Spec.OperationID {
		t.Fatalf("Plan OperationUID = %q, want %q", writer.plan.Value().OperationUID, started.Spec.OperationID)
	}
	digest, err := writer.plan.Digest()
	if err != nil {
		t.Fatalf("Plan.Digest() error = %v", err)
	}
	if digest.String() != started.Spec.PlanDigest {
		t.Fatalf("Plan digest = %q, want %q", digest, started.Spec.PlanDigest)
	}
	if len(result.Events) != 2 {
		t.Fatalf("Events = %#v, want operation and agent plan events", result.Events)
	}
}

func TestWorkflowは再試行時に保存済みDeadlineから同じPlanを再生成する(t *testing.T) {
	input := workflowInput(t)
	starter := &workflowOperationStarter{}
	writer := &recordingPlanWriter{}
	workflow := NewWorkflow(starter, writer, testPlanSigner(t))

	firstResult, err := workflow.Start(t.Context(), input)
	if err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	first := firstResult.Operation
	firstPlanDigest := writer.planDigest(t)

	input.Now = input.Now.Add(time.Hour)
	secondResult, err := workflow.Start(t.Context(), input)
	if err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	second := secondResult.Operation

	if first.UID != second.UID {
		t.Fatalf("Operation UID = %q, want existing %q", second.UID, first.UID)
	}
	if got := writer.planDigest(t); got != firstPlanDigest {
		t.Fatalf("retried Plan digest = %q, want %q", got, firstPlanDigest)
	}
}

func TestWorkflowはKubernetesBinary更新でNodeLifecyclePlanも保存する(t *testing.T) {
	input := workflowInput(t)
	input.CurrentDistributionVersion = "v1.34.0"
	input.TargetDistributionVersion = targetKubernetesVersion
	input.Machine.Spec.Version = targetKubernetesVersion
	input.Manifest = updateManifestWithKubernetesVersion(t, targetKubernetesVersion)
	input.NodeRole = distributiondomain.NodeRoleWorker
	starter := &workflowOperationStarter{}
	agentWriter := &recordingPlanWriter{}
	nodeWriter := &recordingNodeLifecyclePlanWriter{}
	workflow := NewWorkflow(starter, agentWriter, testPlanSigner(t))
	workflow.SetNodeLifecyclePlanWriter(nodeWriter, testPlanSigner(t))

	result, err := workflow.Start(t.Context(), input)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	started := result.Operation

	if started.Spec.UpdateClass != infrastructurev1beta1.UpdateClassKubernetesBinary {
		t.Fatalf("UpdateClass = %q, want KubernetesBinary", started.Spec.UpdateClass)
	}
	if started.Spec.NodeLifecyclePlanDigest == "" {
		t.Fatal("NodeLifecyclePlanDigest is empty")
	}
	if nodeWriter.operation == nil || nodeWriter.operation.UID != started.UID {
		t.Fatalf("written Node Lifecycle Operation UID = %v, want %q", nodeWriter.operation, started.UID)
	}
	nodePlan := nodeWriter.plan.Value()
	if nodePlan.NodeRole != distributiondomain.NodeRoleWorker ||
		nodePlan.CurrentVersion != "v1.34.0" ||
		nodePlan.TargetVersion != targetKubernetesVersion {
		t.Fatalf("Node Lifecycle Plan = %#v, want worker v1.34.0 -> v1.35.0", nodePlan)
	}
	digest, err := nodeWriter.plan.Digest()
	if err != nil {
		t.Fatalf("Node Lifecycle Plan.Digest() error = %v", err)
	}
	if digest.String() != started.Spec.NodeLifecyclePlanDigest {
		t.Fatalf("Node Lifecycle Plan digest = %q, want %q", digest, started.Spec.NodeLifecyclePlanDigest)
	}
	if len(result.Events) != 3 {
		t.Fatalf("Events = %#v, want operation, agent plan, and node lifecycle plan events", result.Events)
	}
}

func workflowInput(t *testing.T) WorkflowInput {
	t.Helper()
	input := updateInput()
	input.PlanDigest = ""
	input.Host.Spec.Architecture = infrastructurev1beta1.ArchitectureAMD64
	input.Host.Spec.PlatformProfile = "amd64-uefi-ab/v1"
	input.Host.Spec.RootDeviceHints.DeviceName = "/dev/disk/by-id/test-root"
	input.Host.Spec.RootDeviceHints.SerialNumber = "SERIAL-1"
	input.Host.Spec.RootDeviceHints.MinSizeBytes = 64 << 30
	manifest := updateManifest(t)
	input.TargetImageDigest = manifest.Value().Image.Digest
	input.TargetArtifactGeneration = manifest.Value().Generation
	return WorkflowInput{
		StartInput: input,
		Manifest:   manifest,
	}
}

func testPlanSigner(t *testing.T) PlanSigner {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	return PlanSigner{KeyID: "plan-key", PrivateKey: privateKey}
}

type workflowOperationStarter struct {
	current *infrastructurev1beta1.TartHostOperation
}

func (starter *workflowOperationStarter) Start(
	_ context.Context,
	desired *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	if starter.current != nil {
		if starter.current.Spec.OperationID != desired.Spec.OperationID {
			return nil, fmt.Errorf("another operation is active")
		}
		return starter.current.DeepCopy(), nil
	}
	starter.current = desired.DeepCopy()
	starter.current.Name = "tarthostoperation-host-a"
	starter.current.UID = types.UID("operation-object-uid")
	return starter.current.DeepCopy(), nil
}

type recordingPlanWriter struct {
	operation *infrastructurev1beta1.TartHostOperation
	plan      agentprotocol.ValidatedPlan
	signature agentprotocol.Signature
}

type recordingNodeLifecyclePlanWriter struct {
	operation *infrastructurev1beta1.TartHostOperation
	plan      nodelifecycleapp.ValidatedPlan
	signature agentprotocol.Signature
}

func (writer *recordingNodeLifecyclePlanWriter) Write(
	_ context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	plan nodelifecycleapp.ValidatedPlan,
	signature agentprotocol.Signature,
) error {
	writer.operation = operation.DeepCopy()
	writer.plan = plan
	writer.signature = signature
	return nil
}

func (writer *recordingPlanWriter) Write(
	_ context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	plan agentprotocol.ValidatedPlan,
	signature agentprotocol.Signature,
) error {
	writer.operation = operation.DeepCopy()
	writer.plan = plan
	writer.signature = signature
	return nil
}

func (writer *recordingPlanWriter) planDigest(t *testing.T) string {
	t.Helper()
	digest, err := writer.plan.Digest()
	if err != nil {
		t.Fatalf("Plan.Digest() error = %v", err)
	}
	return digest.String()
}
