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

package cleaning

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestWorkflowはOperation作成後に一致する署名済みCleaningPlanを保存する(t *testing.T) {
	host := cleaningTestHost()
	machine := &infrastructurev1beta1.TartMachine{}
	machine.Name = "machine-a"
	machine.Namespace = "default"
	machine.UID = types.UID("machine-a-uid")
	machine.Spec.DeletionPolicy = infrastructurev1beta1.DeletionPolicyWipeAll

	hostPhase := &recordingHostPhase{}
	operations := &workflowOperationService{}
	writer := &workflowPlanWriter{}
	workflow := NewWorkflow(hostPhase, operations, writer, cleaningPlanSigner(t))
	workflow.now = func() time.Time { return time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC) }

	started, err := workflow.StartCleaning(t.Context(), machine, host)
	if err != nil {
		t.Fatalf("StartCleaning() error = %v", err)
	}
	if writer.operation == nil || writer.operation.UID != started.UID {
		t.Fatalf("written Operation UID = %v, want %q", writer.operation, started.UID)
	}
	if writer.plan.Value().OperationUID != started.Spec.OperationID {
		t.Fatalf("Plan OperationUID = %q, want %q", writer.plan.Value().OperationUID, started.Spec.OperationID)
	}
	if writer.plan.Value().OperationType != agentprotocol.OperationTypeWipeAll {
		t.Fatalf("Plan OperationType = %q, want WipeAll", writer.plan.Value().OperationType)
	}
	digest, err := writer.plan.Digest()
	if err != nil {
		t.Fatalf("Plan.Digest() error = %v", err)
	}
	if digest.String() != started.Spec.PlanDigest {
		t.Fatalf("Plan digest = %q, want %q", digest, started.Spec.PlanDigest)
	}
}

func TestWorkflowは再試行時に保存済みDeadlineから同じCleaningPlanを再生成する(t *testing.T) {
	host := cleaningTestHost()
	machine := &infrastructurev1beta1.TartMachine{}
	machine.Name = "machine-a"
	machine.Namespace = "default"
	machine.UID = types.UID("machine-a-uid")
	machine.Spec.DeletionPolicy = infrastructurev1beta1.DeletionPolicyRetainData

	hostPhase := &recordingHostPhase{}
	operations := &workflowOperationService{}
	writer := &workflowPlanWriter{}
	workflow := NewWorkflow(hostPhase, operations, writer, cleaningPlanSigner(t))
	workflow.now = func() time.Time { return time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC) }

	first, err := workflow.StartCleaning(t.Context(), machine, host)
	if err != nil {
		t.Fatalf("first StartCleaning() error = %v", err)
	}
	firstDigest := writer.planDigest(t)

	workflow.now = func() time.Time { return time.Date(2026, 7, 12, 11, 0, 0, 0, time.UTC) }
	second, err := workflow.StartCleaning(t.Context(), machine, host)
	if err != nil {
		t.Fatalf("second StartCleaning() error = %v", err)
	}
	if first.UID != second.UID {
		t.Fatalf("Operation UID = %q, want %q", second.UID, first.UID)
	}
	if got := writer.planDigest(t); got != firstDigest {
		t.Fatalf("retried Plan digest = %q, want %q", got, firstDigest)
	}
}

func cleaningPlanSigner(t *testing.T) PlanSigner {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	return PlanSigner{KeyID: "plan-key", PrivateKey: privateKey}
}

type workflowOperationService struct {
	current *infrastructurev1beta1.TartHostOperation
}

func (service *workflowOperationService) Start(
	_ context.Context,
	desired *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	if service.current != nil {
		if service.current.Spec.OperationID != desired.Spec.OperationID {
			return nil, fmt.Errorf("another operation is active")
		}
		return service.current.DeepCopy(), nil
	}
	service.current = desired.DeepCopy()
	service.current.Name = "tarthostoperation-host-a"
	service.current.UID = types.UID("operation-object-uid")
	return service.current.DeepCopy(), nil
}

type workflowPlanWriter struct {
	operation *infrastructurev1beta1.TartHostOperation
	plan      agentprotocol.ValidatedPlan
	signature agentprotocol.Signature
}

func (writer *workflowPlanWriter) Write(
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

func (writer *workflowPlanWriter) planDigest(t *testing.T) string {
	t.Helper()
	digest, err := writer.plan.Digest()
	if err != nil {
		t.Fatalf("Plan.Digest() error = %v", err)
	}
	return digest.String()
}

type recordingHostPhase struct {
	called bool
	policy infrastructurev1beta1.DeletionPolicy
}

func (r *recordingHostPhase) MarkHostCleaningForDeletion(
	_ context.Context,
	_ *infrastructurev1beta1.TartHost,
	policy infrastructurev1beta1.DeletionPolicy,
) error {
	r.called = true
	r.policy = policy
	return nil
}
