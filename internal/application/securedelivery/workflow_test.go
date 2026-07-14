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

package securedelivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type staticOperationResolver struct {
	key       client.ObjectKey
	operation *infrastructurev1beta1.TartHostOperation
}

func (resolver staticOperationResolver) Resolve(
	_ context.Context,
	operationUID string,
) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error) {
	if operationUID != resolver.operation.Spec.OperationID {
		return client.ObjectKey{}, nil, errors.New("not found")
	}
	return resolver.key, resolver.operation.DeepCopy(), nil
}

type staticRegistrationVerifier struct{}

func (staticRegistrationVerifier) Verify(
	context.Context,
	*infrastructurev1beta1.TartHostOperation,
	string,
	agentprotocol.RegisterRequest,
) error {
	return nil
}

type recordingSessionService struct {
	issued bool
	claim  bool
}

func (service *recordingSessionService) Issue(
	context.Context,
	client.ObjectKey,
	string,
	string,
	time.Time,
) (agentsession.Token, time.Time, error) {
	service.issued = true
	return agentsession.Token{}, time.Time{}, nil
}

func (service *recordingSessionService) Authenticate(
	context.Context,
	client.ObjectKey,
	string,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (service *recordingSessionService) ClaimBootstrap(
	context.Context,
	client.ObjectKey,
	string,
	string,
	string,
	time.Time,
) error {
	service.claim = true
	return nil
}

type staticBootstrapProvider struct {
	bundle agentprotocol.BootstrapBundle
}

func (provider staticBootstrapProvider) GetBootstrapBundle(
	context.Context,
	client.ObjectKey,
) (agentprotocol.BootstrapBundle, error) {
	return provider.bundle, nil
}

func TestWorkflowRegisterAndBootstrap(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "operation"},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			PlanDigest:  "sha256:" + strings.Repeat("a", 64),
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host",
				UID:       "host-uid",
			},
		},
	}
	bundle := agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       []byte("#cloud-config\n"),
		PayloadDigest: digest.FromBytes([]byte("#cloud-config\n")).String(),
		MachineUID:    "machine-uid",
		OperationUID:  "operation-uid",
	}
	sessionService := &recordingSessionService{}
	workflow := NewWorkflow(Ports{
		Operations:           staticOperationResolver{key: client.ObjectKey{Namespace: "default", Name: "operation"}, operation: operation},
		RegistrationVerifier: staticRegistrationVerifier{},
		Sessions:             sessionService,
		Bootstrap:            staticBootstrapProvider{bundle: bundle},
		Now:                  func() time.Time { return now },
	})

	registerResult, err := workflow.Register(t.Context(), RegisterInput{
		OperationUID:  "operation-uid",
		Authorization: "",
		Request: agentprotocol.RegisterRequest{
			APIVersion:      agentprotocol.APIVersion,
			OperationUID:    "operation-uid",
			HostUID:         "host-uid",
			AgentInstanceID: "agent-1",
		},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, ok := registerResult.(RegisterAccepted); !ok {
		t.Fatalf("Register() = %#v, want RegisterAccepted", registerResult)
	}
	if !sessionService.issued {
		t.Fatal("register did not issue a session")
	}

	bootstrapResult, err := workflow.Bootstrap(t.Context(), BootstrapInput{
		OperationUID:  "operation-uid",
		Authorization: "Bearer token",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if _, ok := bootstrapResult.(BootstrapAccepted); !ok {
		t.Fatalf("Bootstrap() = %#v, want BootstrapAccepted", bootstrapResult)
	}
	if !sessionService.claim {
		t.Fatal("bootstrap did not claim the session")
	}
}

func TestWorkflowRejectsInvalidRegisterRequest(t *testing.T) {
	workflow := NewWorkflow(Ports{})
	result, err := workflow.Register(t.Context(), RegisterInput{
		Request: agentprotocol.RegisterRequest{
			APIVersion: agentprotocol.APIVersion,
		},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	rejected, ok := result.(RegisterRejected)
	if !ok || rejected.Status != 422 {
		t.Fatalf("Register() = %#v, want 422 rejection", result)
	}
}
