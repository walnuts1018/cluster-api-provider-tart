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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

// PlanSignerはAgent Plan専用の署名鍵を保持する。
type PlanSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
}

// WorkflowInputはUpdate OperationとPlanを同時に開始する入力である。
type Command struct {
	StartInput
	Manifest artifact.ValidatedManifest
}

type Event struct{ Result StartResult }

// WorkflowはUpdate Operation作成と署名済みPlan保存を順序付ける。
type Workflow struct {
	effects             *effectRunner
	signer              PlanSigner
	nodeLifecycleSigner PlanSigner
}

// NewWorkflowはUpdate workflowを生成する。
func NewWorkflow(
	operations OperationStarter,
	plans PlanWriter,
	signer PlanSigner,
	nodeLifecyclePlans NodeLifecyclePlanWriter,
	nodeLifecycleSigner PlanSigner,
) *Workflow {
	workflow := &Workflow{
		effects: &effectRunner{
			operations: operations,
			plans:      plans,
		},
		signer:              signer,
		nodeLifecycleSigner: nodeLifecycleSigner,
	}
	workflow.effects.setNodeLifecyclePlanWriter(nodeLifecyclePlans)
	return workflow
}

// StartはOperationを冪等に作成し、そのOperationを正本としてPlanを保存する。
func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "update machine", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](Event{Result: result})
}

func (workflow *Workflow) execute(ctx context.Context, input Command) (StartResult, error) {
	draft, err := BuildOperationDraft(input.StartInput)
	if err != nil {
		return StartResult{}, err
	}

	started, hasNodeLifecyclePlan, err := workflow.effects.start(ctx, startOperationEffect{
		Operation: draft,
		BuildAgentPlan: func(operation *infrastructurev1beta1.TartHostOperation) (SignedAgentPlan, error) {
			plan, err := buildSignedAgentPlanStep(input, operation, workflow.signer)
			return SignedAgentPlan(plan), err
		},
		BuildNodeLifecyclePlan: func(operation *infrastructurev1beta1.TartHostOperation) (SignedNodeLifecyclePlan, bool, error) {
			plan, ok, err := buildSignedNodeLifecyclePlanStep(input, operation, workflow.nodeLifecycleSigner)
			return SignedNodeLifecyclePlan(plan), ok, err
		},
	})
	if err != nil {
		return StartResult{}, err
	}
	events := []DomainEvent{
		OperationStarted{OperationID: started.Spec.OperationID},
		AgentPlanPersisted{OperationID: started.Spec.OperationID},
	}
	if hasNodeLifecyclePlan {
		events = append(events, NodeLifecyclePlanPersisted{OperationID: started.Spec.OperationID})
	}
	return StartResult{
		Operation: started,
		Events:    events,
	}, nil
}
