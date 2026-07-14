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
	inplaceupdateevent "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/event"
	inplaceupdatehandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/handler"
	inplaceupdatemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/model"
	inplaceupdateport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/port"
	inplaceupdatestep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/step"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

type PlanWriter = inplaceupdateport.PlanWriter
type NodeLifecyclePlanWriter = inplaceupdateport.NodeLifecyclePlanWriter
type OperationStarter = inplaceupdateport.OperationStarter

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

type Step = inplaceupdatestep.Step
type StepStartOperation = inplaceupdatestep.StartOperation
type StepPersistAgentPlan = inplaceupdatestep.PersistAgentPlan
type StepPersistNodeLifecyclePlan = inplaceupdatestep.PersistNodeLifecyclePlan

type Event = inplaceupdateevent.Event
type EventOperationStarted = inplaceupdateevent.OperationStarted
type EventAgentPlanPersisted = inplaceupdateevent.AgentPlanPersisted
type EventNodeLifecyclePlanPersisted = inplaceupdateevent.NodeLifecyclePlanPersisted

type StartResult = inplaceupdatemodel.StartResult

// WorkflowはUpdate Operation作成と署名済みPlan保存を順序付ける。
type Workflow struct {
	steps               *inplaceupdatestep.Executor
	commands            *inplaceupdatehandler.CommandHandler
	signer              PlanSigner
	nodeLifecycleSigner PlanSigner
}

// NewWorkflowはUpdate workflowを生成する。
func NewWorkflow(
	operations OperationStarter,
	plans PlanWriter,
	signer PlanSigner,
) *Workflow {
	steps := inplaceupdatestep.NewExecutor(operations, plans)
	return &Workflow{
		steps:    steps,
		commands: inplaceupdatehandler.NewCommandHandler(steps),
		signer:   signer,
	}
}

// SetNodeLifecyclePlanWriterはKubernetesBinary更新で配信するNode Lifecycle Plan保存先を設定する。
func (workflow *Workflow) SetNodeLifecyclePlanWriter(
	plans NodeLifecyclePlanWriter,
	signer PlanSigner,
) {
	workflow.steps.SetNodeLifecyclePlanWriter(plans)
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

	started, hasNodeLifecyclePlan, err := workflow.commands.Start(ctx, inplaceupdatehandler.StartCommand{
		Operation: draft,
		BuildAgentPlan: func(operation *infrastructurev1beta1.TartHostOperation) (inplaceupdatehandler.SignedAgentPlan, error) {
			plan, err := buildSignedAgentPlanStep(input, operation, workflow.signer)
			return inplaceupdatehandler.SignedAgentPlan(plan), err
		},
		BuildNodeLifecyclePlan: func(operation *infrastructurev1beta1.TartHostOperation) (inplaceupdatehandler.SignedNodeLifecyclePlan, bool, error) {
			plan, ok, err := buildSignedNodeLifecyclePlanStep(input, operation, workflow.nodeLifecycleSigner)
			return inplaceupdatehandler.SignedNodeLifecyclePlan(plan), ok, err
		},
	})
	if err != nil {
		return StartResult{}, err
	}
	events := []inplaceupdateevent.Event{
		EventOperationStarted{OperationID: started.Spec.OperationID},
		EventAgentPlanPersisted{OperationID: started.Spec.OperationID},
	}
	if hasNodeLifecyclePlan {
		events = append(events, EventNodeLifecyclePlanPersisted{OperationID: started.Spec.OperationID})
	}
	return inplaceupdatemodel.StartResult{
		Operation: started,
		Events:    events,
	}, nil
}
