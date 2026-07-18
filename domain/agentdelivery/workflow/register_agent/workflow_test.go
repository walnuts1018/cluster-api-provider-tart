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

package registeragent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentsession "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/agentsession"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

func TestWorkflowは有効な登録要求にSessionを発行する(t *testing.T) {
	t.Parallel()

	operation := testOperation()
	sessions := &sessionIssuerStub{}
	workflow := NewWorkflow(operationResolverStub{operation: operation}, registrationVerifierStub{}, sessions, nil)
	outcome := workflow.Do(t.Context(), Command{
		OperationUID: operation.Spec.OperationID,
		Request: agentprotocol.RegisterRequest{
			APIVersion:      agentprotocol.APIVersion,
			OperationUID:    operation.Spec.OperationID,
			HostUID:         "host-uid",
			AgentInstanceID: "agent-1",
		},
	})
	event, present := outcome.Value().Value()
	if !present {
		t.Fatalf("Do() = %#v, want success", outcome)
	}
	if _, ok := event.(AgentRegistered); !ok {
		t.Fatalf("Do() = %#v, want AgentRegistered", event)
	}
	if !sessions.issued {
		t.Fatal("session was not issued")
	}
}

func TestWorkflowは不正な登録要求を拒否する(t *testing.T) {
	t.Parallel()

	outcome := NewWorkflow(nil, nil, nil, nil).Do(t.Context(), Command{Request: agentprotocol.RegisterRequest{APIVersion: agentprotocol.APIVersion}})
	event, present := outcome.Value().Value()
	if !present {
		t.Fatalf("Do() = %#v, want success", outcome)
	}
	if rejected, ok := event.(AgentRegistrationRejected); !ok || rejected.Status != 422 {
		t.Fatalf("Do() = %#v, want 422 rejection", event)
	}
}

type operationResolverStub struct {
	operation *infrastructurev1beta1.TartHostOperation
}

func (stub operationResolverStub) Resolve(context.Context, string) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error) {
	if stub.operation == nil {
		return client.ObjectKey{}, nil, errors.New("not found")
	}
	return client.ObjectKey{Namespace: stub.operation.Namespace, Name: stub.operation.Name}, stub.operation.DeepCopy(), nil
}

type registrationVerifierStub struct{}

func (registrationVerifierStub) Verify(context.Context, *infrastructurev1beta1.TartHostOperation, string, agentprotocol.RegisterRequest) error {
	return nil
}

type sessionIssuerStub struct {
	issued bool
}

func (stub *sessionIssuerStub) Issue(context.Context, client.ObjectKey, string, string, time.Time) (agentsession.Token, time.Time, error) {
	stub.issued = true
	return agentsession.Token{}, time.Time{}, nil
}

func testOperation() *infrastructurev1beta1.TartHostOperation {
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "operation"},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			PlanDigest:  "sha256:" + strings.Repeat("a", 64),
		},
	}
}
