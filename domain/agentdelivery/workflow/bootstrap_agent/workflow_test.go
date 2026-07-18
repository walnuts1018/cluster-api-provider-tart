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

package bootstrapagent

import (
	"context"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

func TestWorkflowはBootstrap取得後にSessionを消費する(t *testing.T) {
	t.Parallel()

	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "operation"},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			HostRef:     infrastructurev1beta1.ResourceReference{UID: "host-uid"},
		},
	}
	payload := []byte("#cloud-config\n")
	sessions := &sessionClaimerStub{}
	workflow := NewWorkflow(
		operationResolverStub{operation: operation},
		sessions,
		bootstrapProviderStub{bundle: agentprotocol.BootstrapBundle{
			APIVersion:    agentprotocol.APIVersion,
			Format:        agentprotocol.BootstrapFormatCloud,
			Payload:       payload,
			PayloadDigest: digest.FromBytes(payload).String(),
			MachineUID:    "machine-uid",
			OperationUID:  operation.Spec.OperationID,
		}},
		nil,
	)
	outcome := workflow.Do(t.Context(), Command{OperationUID: operation.Spec.OperationID, Authorization: "Bearer token"})
	event, present := outcome.Value().Value()
	if !present {
		t.Fatalf("Do() = %#v, want success", outcome)
	}
	if _, ok := event.(BootstrapDelivered); !ok {
		t.Fatalf("Do() = %#v, want BootstrapDelivered", event)
	}
	if !sessions.claimed {
		t.Fatal("session was not claimed")
	}
}

type operationResolverStub struct {
	operation *infrastructurev1beta1.TartHostOperation
}

func (stub operationResolverStub) Resolve(context.Context, string) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error) {
	return client.ObjectKey{Namespace: stub.operation.Namespace, Name: stub.operation.Name}, stub.operation.DeepCopy(), nil
}

type sessionClaimerStub struct {
	claimed bool
}

func (stub *sessionClaimerStub) ClaimBootstrap(context.Context, client.ObjectKey, string, string, string, time.Time) error {
	stub.claimed = true
	return nil
}

type bootstrapProviderStub struct {
	bundle agentprotocol.BootstrapBundle
}

func (stub bootstrapProviderStub) GetBootstrapBundle(context.Context, client.ObjectKey) (agentprotocol.BootstrapBundle, error) {
	return stub.bundle, nil
}
