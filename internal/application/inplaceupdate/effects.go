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
	"fmt"

	"github.com/opencontainers/go-digest"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	nodelifecycleapp "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type SignedAgentPlan struct {
	Plan      agentprotocol.ValidatedPlan
	Signature agentprotocol.Signature
	Digest    digest.Digest
}

type SignedNodeLifecyclePlan struct {
	Plan      nodelifecycleapp.ValidatedPlan
	Signature agentprotocol.Signature
	Digest    digest.Digest
}

type startOperationEffect struct {
	Operation              *infrastructurev1beta1.TartHostOperation
	BuildAgentPlan         func(*infrastructurev1beta1.TartHostOperation) (SignedAgentPlan, error)
	BuildNodeLifecyclePlan func(*infrastructurev1beta1.TartHostOperation) (SignedNodeLifecyclePlan, bool, error)
}

type effectRunner struct {
	operations         OperationStarter
	plans              PlanWriter
	nodeLifecyclePlans NodeLifecyclePlanWriter
}

func (runner *effectRunner) setNodeLifecyclePlanWriter(writer NodeLifecyclePlanWriter) {
	runner.nodeLifecyclePlans = writer
}

func (runner *effectRunner) start(
	ctx context.Context,
	command startOperationEffect,
) (*infrastructurev1beta1.TartHostOperation, bool, error) {
	if command.BuildAgentPlan == nil {
		return nil, false, fmt.Errorf("build Update Plan step is not configured")
	}
	if command.BuildNodeLifecyclePlan == nil {
		return nil, false, fmt.Errorf("build Node Lifecycle Plan step is not configured")
	}
	candidatePlan, err := command.BuildAgentPlan(command.Operation)
	if err != nil {
		return nil, false, err
	}
	command.Operation.Spec.PlanDigest = candidatePlan.Digest.String()
	candidateNodePlan, hasCandidateNodePlan, err := command.BuildNodeLifecyclePlan(command.Operation)
	if err != nil {
		return nil, false, err
	}
	if hasCandidateNodePlan {
		command.Operation.Spec.NodeLifecyclePlanDigest = candidateNodePlan.Digest.String()
	}

	started, err := runner.operations.Start(ctx, command.Operation)
	if err != nil {
		return nil, false, fmt.Errorf("start Update Operation: %w", err)
	}
	persistedPlan, err := command.BuildAgentPlan(started)
	if err != nil {
		return nil, false, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return nil, false, fmt.Errorf("stored Update Operation Plan digest does not match regenerated Plan")
	}
	persistedNodePlan, hasPersistedNodePlan, err := command.BuildNodeLifecyclePlan(started)
	if err != nil {
		return nil, false, err
	}
	if hasPersistedNodePlan && persistedNodePlan.Digest.String() != started.Spec.NodeLifecyclePlanDigest {
		return nil, false, fmt.Errorf("stored Update Operation Node Lifecycle Plan digest does not match regenerated Plan")
	}
	if err := runner.plans.Write(ctx, started, persistedPlan.Plan, persistedPlan.Signature); err != nil {
		return nil, false, fmt.Errorf("persist Update Plan: %w", err)
	}
	if hasPersistedNodePlan {
		if runner.nodeLifecyclePlans == nil {
			return nil, false, fmt.Errorf("node Lifecycle Plan writer is required for KubernetesBinary update")
		}
		if err := runner.nodeLifecyclePlans.Write(
			ctx,
			started,
			persistedNodePlan.Plan,
			persistedNodePlan.Signature,
		); err != nil {
			return nil, false, fmt.Errorf("persist Node Lifecycle Plan: %w", err)
		}
	}
	return started, hasPersistedNodePlan, nil
}
