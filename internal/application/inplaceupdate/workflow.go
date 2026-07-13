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
	nodelifecycleapp "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	distributiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
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

// NodeLifecyclePlanWriterはOperationに対応する署名済みNode Lifecycle Planを永続化する境界である。
type NodeLifecyclePlanWriter interface {
	Write(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
		nodelifecycleapp.ValidatedPlan,
		agentprotocol.Signature,
	) error
}

// OperationStarterはOperation作成の永続化境界である。
type OperationStarter interface {
	Start(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
	) (*infrastructurev1beta1.TartHostOperation, error)
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

type Step interface {
	isInPlaceUpdateStep()
}

type StepStartOperation struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

func (StepStartOperation) isInPlaceUpdateStep() {}

type StepPersistAgentPlan struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Plan      agentprotocol.ValidatedPlan
	Signature agentprotocol.Signature
}

func (StepPersistAgentPlan) isInPlaceUpdateStep() {}

type StepPersistNodeLifecyclePlan struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Plan      nodelifecycleapp.ValidatedPlan
	Signature agentprotocol.Signature
}

func (StepPersistNodeLifecyclePlan) isInPlaceUpdateStep() {}

type Event interface {
	isInPlaceUpdateEvent()
}

type EventOperationStarted struct {
	OperationID string
}

func (EventOperationStarted) isInPlaceUpdateEvent() {}

type EventAgentPlanPersisted struct {
	OperationID string
}

func (EventAgentPlanPersisted) isInPlaceUpdateEvent() {}

type EventNodeLifecyclePlanPersisted struct {
	OperationID string
}

func (EventNodeLifecyclePlanPersisted) isInPlaceUpdateEvent() {}

type StartResult struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Events    []Event
}

// WorkflowはUpdate Operation作成と署名済みPlan保存を順序付ける。
type Workflow struct {
	operations          OperationStarter
	plans               PlanWriter
	signer              PlanSigner
	nodeLifecyclePlans  NodeLifecyclePlanWriter
	nodeLifecycleSigner PlanSigner
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

// SetNodeLifecyclePlanWriterはKubernetesBinary更新で配信するNode Lifecycle Plan保存先を設定する。
func (workflow *Workflow) SetNodeLifecyclePlanWriter(
	plans NodeLifecyclePlanWriter,
	signer PlanSigner,
) {
	workflow.nodeLifecyclePlans = plans
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
	candidatePlan, err := workflow.buildPlan(input, draft)
	if err != nil {
		return StartResult{}, err
	}
	draft.Spec.PlanDigest = candidatePlan.Digest.String()
	candidateNodePlan, hasCandidateNodePlan, err := workflow.buildNodeLifecyclePlan(input, draft)
	if err != nil {
		return StartResult{}, err
	}
	if hasCandidateNodePlan {
		draft.Spec.NodeLifecyclePlanDigest = candidateNodePlan.Digest.String()
	}

	started, err := workflow.operations.Start(ctx, draft)
	if err != nil {
		return StartResult{}, fmt.Errorf("start Update Operation: %w", err)
	}
	persistedPlan, err := workflow.buildPlan(input, started)
	if err != nil {
		return StartResult{}, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return StartResult{}, fmt.Errorf("stored Update Operation Plan digest does not match regenerated Plan")
	}
	persistedNodePlan, hasPersistedNodePlan, err := workflow.buildNodeLifecyclePlan(input, started)
	if err != nil {
		return StartResult{}, err
	}
	if hasPersistedNodePlan && persistedNodePlan.Digest.String() != started.Spec.NodeLifecyclePlanDigest {
		return StartResult{}, fmt.Errorf("stored Update Operation Node Lifecycle Plan digest does not match regenerated Plan")
	}
	if err := workflow.plans.Write(
		ctx,
		started,
		persistedPlan.Plan,
		persistedPlan.Signature,
	); err != nil {
		return StartResult{}, fmt.Errorf("persist Update Plan: %w", err)
	}
	events := []Event{
		EventOperationStarted{OperationID: started.Spec.OperationID},
		EventAgentPlanPersisted{OperationID: started.Spec.OperationID},
	}
	if hasPersistedNodePlan {
		if workflow.nodeLifecyclePlans == nil {
			return StartResult{}, fmt.Errorf("Node Lifecycle Plan writer is required for KubernetesBinary update")
		}
		if err := workflow.nodeLifecyclePlans.Write(
			ctx,
			started,
			persistedNodePlan.Plan,
			persistedNodePlan.Signature,
		); err != nil {
			return StartResult{}, fmt.Errorf("persist Node Lifecycle Plan: %w", err)
		}
		events = append(events, EventNodeLifecyclePlanPersisted{OperationID: started.Spec.OperationID})
	}
	return StartResult{
		Operation: started,
		Events:    events,
	}, nil
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

func (workflow *Workflow) buildNodeLifecyclePlan(
	input WorkflowInput,
	operation *infrastructurev1beta1.TartHostOperation,
) (nodelifecycleapp.BuiltPlan, bool, error) {
	if operation.Spec.UpdateClass == infrastructurev1beta1.UpdateClassOSOnly {
		return nodelifecycleapp.BuiltPlan{}, false, nil
	}
	if operation.Spec.UpdateClass != infrastructurev1beta1.UpdateClassKubernetesBinary {
		return nodelifecycleapp.BuiltPlan{}, false, fmt.Errorf("unsupported distribution lifecycle update class %q", operation.Spec.UpdateClass)
	}
	nodeRole := input.NodeRole
	if nodeRole == "" {
		nodeRole = distributiondomain.NodeRoleWorker
	}
	plan, err := distributiondomain.BuildPlan(distributiondomain.PlanInput{
		OperationID:    operation.Spec.OperationID,
		CurrentVersion: currentDistributionVersion(input.StartInput),
		TargetVersion:  targetDistributionVersion(input.StartInput),
		UpdateClass:    distributiondomain.UpdateClassKubernetesBinary,
		NodeRole:       nodeRole,
	})
	if err != nil {
		return nodelifecycleapp.BuiltPlan{}, false, fmt.Errorf("build Node Lifecycle domain Plan: %w", err)
	}
	built, err := nodelifecycleapp.BuildSignedPlan(
		plan,
		operation.Spec.Deadline.Time,
		workflow.nodeLifecycleSigner.KeyID,
		workflow.nodeLifecycleSigner.PrivateKey,
	)
	if err != nil {
		return nodelifecycleapp.BuiltPlan{}, false, fmt.Errorf("build signed Node Lifecycle Plan: %w", err)
	}
	return built, true, nil
}
