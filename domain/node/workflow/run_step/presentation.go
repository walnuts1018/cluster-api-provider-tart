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

package nodelifecycleengine

import (
	"fmt"
	"strings"

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/node/entity/nodelifecycleengine"
)

type SnapshotResult struct {
	Ref             string
	RestoreVerified bool
}

type StepResult struct {
	SnapshotRef string
}

func presentStepDecision(decision domain.RunnableDecision) error {
	switch decided := decision.(type) {
	case domain.StepRunnable:
		return nil
	case domain.StepBlocked:
		return fmt.Errorf("node lifecycle engine step blocked: %s", failureMessage(decided.Failure))
	default:
		return fmt.Errorf("unsupported node lifecycle engine step decision %T", decision)
	}
}

func presentSnapshot(snapshot SnapshotResult) (StepResult, error) {
	if snapshot.Ref == "" {
		return StepResult{}, fmt.Errorf("snapshot reference is required")
	}
	if !snapshot.RestoreVerified {
		return StepResult{}, fmt.Errorf("snapshot restore test must pass before using SnapshotRef")
	}
	return StepResult{SnapshotRef: snapshot.Ref}, nil
}

func presentHealth(decision domain.HealthDecision) (StepResult, error) {
	switch decided := decision.(type) {
	case domain.HealthGateSatisfied:
		return StepResult{}, nil
	case domain.HealthGateBlocked:
		reasons := make([]string, 0, len(decided.Failures))
		for _, failure := range decided.Failures {
			reasons = append(reasons, failureMessage(failure))
		}
		return StepResult{}, fmt.Errorf("distribution health gate failed: %s", strings.Join(reasons, ", "))
	default:
		return StepResult{}, fmt.Errorf("unsupported node lifecycle engine health decision %T", decision)
	}
}

func failureMessage(failure domain.Failure) string {
	switch value := failure.(type) {
	case domain.InvalidCurrentVersion:
		return fmt.Sprintf("invalid current version %q", value.Value)
	case domain.InvalidTargetVersion:
		return fmt.Sprintf("invalid target version %q", value.Value)
	case domain.MajorVersionChangeUnsupported:
		return "Kubernetes major version changes are not supported"
	case domain.VersionDowngradeUnsupported:
		return "Kubernetes version downgrade is not supported"
	case domain.MinorVersionSkipUnsupported:
		return "Kubernetes minor version cannot skip more than one minor"
	case domain.WorkerControlPlaneOrderUnsatisfied:
		return "worker lifecycle update requires control plane to accept target version first"
	case domain.SnapshotRequired:
		return fmt.Sprintf("snapshot is required before %s", value.Step)
	case domain.StepNotInPlan:
		return fmt.Sprintf("lifecycle step %q is not part of this plan", value.Step)
	case domain.NodeNotReady:
		return "node is not Ready"
	case domain.VersionMismatch:
		return fmt.Sprintf("node version %q does not match target %q", value.Current, value.Target)
	case domain.StaticPodsNotReady:
		return "static pods are not ready"
	case domain.EtcdQuorumLost:
		return "etcd quorum is not healthy"
	case domain.APIUnhealthy:
		return "API server /readyz is unhealthy"
	default:
		return fmt.Sprintf("unknown failure %T", failure)
	}
}
