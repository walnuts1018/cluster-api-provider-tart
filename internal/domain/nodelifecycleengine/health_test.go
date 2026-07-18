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

import "testing"

func TestEvaluateHealthは全Gate成功時だけCommitを許可する(t *testing.T) {
	result := EvaluateHealth(HealthInput{
		NodeReady:       true,
		NodeVersion:     "v1.36.0",
		TargetVersion:   "v1.36.0",
		StaticPodsReady: true,
		EtcdQuorum:      true,
		APIHealthy:      true,
		NodeRole:        NodeRoleControlPlane,
	})

	if !result.CommitAllowed {
		t.Fatalf("CommitAllowed = false, failures = %#v", result.Failures)
	}
}

func TestEvaluateHealthは失敗GateがあればCommitを拒否する(t *testing.T) {
	tests := []struct {
		name  string
		input HealthInput
		want  HealthFailure
	}{
		{
			name: "Node Ready失敗",
			input: HealthInput{
				NodeVersion:     "v1.36.0",
				TargetVersion:   "v1.36.0",
				StaticPodsReady: true,
				EtcdQuorum:      true,
				APIHealthy:      true,
				NodeRole:        NodeRoleControlPlane,
			},
			want: HealthFailureNodeNotReady,
		},
		{
			name: "期待version不一致",
			input: HealthInput{
				NodeReady:       true,
				NodeVersion:     "v1.34.0",
				TargetVersion:   "v1.36.0",
				StaticPodsReady: true,
				EtcdQuorum:      true,
				APIHealthy:      true,
				NodeRole:        NodeRoleControlPlane,
			},
			want: HealthFailureVersionMismatch,
		},
		{
			name: "static Pod失敗",
			input: HealthInput{
				NodeReady:     true,
				NodeVersion:   "v1.36.0",
				TargetVersion: "v1.36.0",
				EtcdQuorum:    true,
				APIHealthy:    true,
				NodeRole:      NodeRoleControlPlane,
			},
			want: HealthFailureStaticPodsNotReady,
		},
		{
			name: "etcd quorum失敗",
			input: HealthInput{
				NodeReady:       true,
				NodeVersion:     "v1.36.0",
				TargetVersion:   "v1.36.0",
				StaticPodsReady: true,
				APIHealthy:      true,
				NodeRole:        NodeRoleControlPlane,
			},
			want: HealthFailureEtcdQuorumLost,
		},
		{
			name: "API health失敗",
			input: HealthInput{
				NodeReady:       true,
				NodeVersion:     "v1.36.0",
				TargetVersion:   "v1.36.0",
				StaticPodsReady: true,
				EtcdQuorum:      true,
				NodeRole:        NodeRoleControlPlane,
			},
			want: HealthFailureAPIUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateHealth(tt.input)
			if result.CommitAllowed {
				t.Fatalf("CommitAllowed = true, want false")
			}
			if !result.HasFailure(tt.want) {
				t.Fatalf("failures = %#v, want %q", result.Failures, tt.want)
			}
		})
	}
}

func TestEvaluateHealthはWorkerでControlPlane専用Gateを要求しない(t *testing.T) {
	result := EvaluateHealth(HealthInput{
		NodeReady:     true,
		NodeVersion:   "v1.36.0",
		TargetVersion: "v1.36.0",
		NodeRole:      NodeRoleWorker,
	})

	if !result.CommitAllowed {
		t.Fatalf("CommitAllowed = false, failures = %#v", result.Failures)
	}
}
