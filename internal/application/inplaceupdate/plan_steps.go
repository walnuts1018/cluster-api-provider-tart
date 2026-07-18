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
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	nodelifecycleapp "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	distributiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

func buildSignedAgentPlanStep(
	input WorkflowInput,
	operation *infrastructurev1beta1.TartHostOperation,
	signer PlanSigner,
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
	}, signer.KeyID, signer.PrivateKey)
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("build signed Update Plan: %w", err)
	}
	return plan, nil
}

func buildSignedNodeLifecyclePlanStep(
	input WorkflowInput,
	operation *infrastructurev1beta1.TartHostOperation,
	signer PlanSigner,
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
		Distribution:   distributiondomain.Distribution(input.Manifest.Value().Kubernetes.Distribution),
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
		signer.KeyID,
		signer.PrivateKey,
	)
	if err != nil {
		return nodelifecycleapp.BuiltPlan{}, false, fmt.Errorf("build signed Node Lifecycle Plan: %w", err)
	}
	return built, true, nil
}
