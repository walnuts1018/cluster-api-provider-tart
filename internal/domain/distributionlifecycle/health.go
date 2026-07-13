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

package distributionlifecycle

import "slices"

// HealthInputはDistribution Lifecycle Commit前の観測値である。
type HealthInput struct {
	NodeReady     bool
	NodeVersion   string
	TargetVersion string

	StaticPodsReady bool
	EtcdQuorum      bool
	APIHealthy      bool

	NodeRole NodeRole
}

type HealthFailure string

const (
	HealthFailureNodeNotReady       HealthFailure = "NodeNotReady"
	HealthFailureVersionMismatch    HealthFailure = "VersionMismatch"
	HealthFailureStaticPodsNotReady HealthFailure = "StaticPodsNotReady"
	HealthFailureEtcdQuorumLost     HealthFailure = "EtcdQuorumLost"
	HealthFailureAPIUnhealthy       HealthFailure = "APIUnhealthy"
)

// HealthResultはCommit可否と拒否理由を保持する。
type HealthResult struct {
	CommitAllowed bool
	Failures      []HealthFailure
}

func (result HealthResult) HasFailure(failure HealthFailure) bool {
	return slices.Contains(result.Failures, failure)
}

// EvaluateHealthはNode観測値からDistribution LifecycleをCommit可能か判定する。
func EvaluateHealth(input HealthInput) HealthResult {
	failures := make([]HealthFailure, 0)
	if !input.NodeReady {
		failures = append(failures, HealthFailureNodeNotReady)
	}
	if input.NodeVersion != input.TargetVersion {
		failures = append(failures, HealthFailureVersionMismatch)
	}
	if input.NodeRole == NodeRoleControlPlane {
		if !input.StaticPodsReady {
			failures = append(failures, HealthFailureStaticPodsNotReady)
		}
		if !input.EtcdQuorum {
			failures = append(failures, HealthFailureEtcdQuorumLost)
		}
		if !input.APIHealthy {
			failures = append(failures, HealthFailureAPIUnhealthy)
		}
	}
	return HealthResult{
		CommitAllowed: len(failures) == 0,
		Failures:      failures,
	}
}
