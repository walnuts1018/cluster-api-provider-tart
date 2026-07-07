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
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

// PlanWriterはOperationに対応する署名済みPlanを永続化する境界である。
type PlanWriter interface {
	Write(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
		agentprotocol.ValidatedPlan,
		agentprotocol.Signature,
	) error
}

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
	operations OperationStarter
	plans      PlanWriter
	signer     PlanSigner
}

// NewWorkflowはUpdate workflowを生成する。
func NewWorkflow(
	operations OperationStarter,
	plans PlanWriter,
	signer PlanSigner,
) *Workflow {
	return &Workflow{
		operations: operations,
		plans:      plans,
		signer:     signer,
	}
}

// StartはOperationを冪等に作成し、そのOperationを正本としてPlanを保存する。
func (workflow *Workflow) Start(
	ctx context.Context,
	input WorkflowInput,
) (*infrastructurev1beta1.TartHostOperation, error) {
	draft, err := BuildOperationDraft(input.StartInput)
	if err != nil {
		return nil, err
	}
	candidatePlan, err := workflow.buildPlan(input, draft)
	if err != nil {
		return nil, err
	}
	draft.Spec.PlanDigest = candidatePlan.Digest.String()

	started, err := workflow.operations.Start(ctx, draft)
	if err != nil {
		return nil, fmt.Errorf("start Update Operation: %w", err)
	}
	persistedPlan, err := workflow.buildPlan(input, started)
	if err != nil {
		return nil, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return nil, fmt.Errorf("stored Update Operation Plan digest does not match regenerated Plan")
	}
	if err := workflow.plans.Write(
		ctx,
		started,
		persistedPlan.Plan,
		persistedPlan.Signature,
	); err != nil {
		return nil, fmt.Errorf("persist Update Plan: %w", err)
	}
	return started, nil
}

func (workflow *Workflow) buildPlan(
	input WorkflowInput,
	operation *infrastructurev1beta1.TartHostOperation,
) (SignedUpdatePlan, error) {
	plan, err := BuildUpdatePlan(UpdatePlanInput{
		OperationID:              operation.Spec.OperationID,
		Machine:                  input.Machine,
		TartMachine:              input.TartMachine,
		Host:                     input.Host,
		Deadline:                 operation.Spec.Deadline.Time,
		Manifest:                 input.Manifest,
		TargetImageDigest:        input.TargetImageDigest,
		TargetArtifactGeneration: input.TargetArtifactGeneration,
	}, workflow.signer.KeyID, workflow.signer.PrivateKey)
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("build signed Update Plan: %w", err)
	}
	return plan, nil
}
