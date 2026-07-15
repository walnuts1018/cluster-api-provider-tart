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
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

// PlanSignerはAgent Plan専用の署名鍵を保持する。
type PlanSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
}

// WorkflowInputはUpdate OperationとPlanを同時に開始する入力である。
type WorkflowInput struct {
	StartInput
	Manifest artifact.ValidatedManifest
}

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
) *Workflow {
	return &Workflow{
		effects: &effectRunner{
			operations: operations,
			plans:      plans,
		},
		signer: signer,
	}
}

// SetNodeLifecyclePlanWriterはKubernetesBinary更新で配信するNode Lifecycle Plan保存先を設定する。
func (workflow *Workflow) SetNodeLifecyclePlanWriter(
	plans NodeLifecyclePlanWriter,
	signer PlanSigner,
) {
	workflow.effects.setNodeLifecyclePlanWriter(plans)
	workflow.nodeLifecycleSigner = signer
}

// StartはOperationを冪等に作成し、そのOperationを正本としてPlanを保存する。
func (workflow *Workflow) Start(
	ctx context.Context,
	input WorkflowInput,
) (StartResult, error) {
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
	events := []Event{
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
