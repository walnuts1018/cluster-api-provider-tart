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

package kubeadm

import (
	"context"
	"testing"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/nodelifecycleengine"
)

func TestRuntimeは型付きkubeadm操作だけを実行する(t *testing.T) {
	runner := &recordingCommandRunner{}
	runtime := NewRuntimeForTest(RuntimeConfig{KubeadmPath: "kubeadm"}, runner)

	if err := runtime.UpgradePlan(t.Context(), "v1.36.0"); err != nil {
		t.Fatalf("UpgradePlan() error = %v", err)
	}
	if err := runtime.UpgradeApply(t.Context(), "v1.36.0"); err != nil {
		t.Fatalf("UpgradeApply() error = %v", err)
	}
	if err := runtime.UpgradeNode(t.Context(), "v1.36.0"); err != nil {
		t.Fatalf("UpgradeNode() error = %v", err)
	}

	want := []commandCall{
		{name: "kubeadm", args: []string{"upgrade", "plan", "v1.36.0"}},
		{name: "kubeadm", args: []string{"upgrade", "apply", "-y", "v1.36.0"}},
		{name: "kubeadm", args: []string{"upgrade", "node"}},
	}
	if !equalCommandCalls(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestRuntimeはSnapshotを固定パスへ保存して検証する(t *testing.T) {
	runner := &recordingCommandRunner{}
	snapshotDir := t.TempDir()
	runtime := NewRuntimeForTest(RuntimeConfig{
		EtcdctlPath: "etcdctl",
		EtcdutlPath: "etcdutl",
		SnapshotDir: snapshotDir,
	}, runner)

	ref, err := runtime.SaveEtcdSnapshot(t.Context(), "operation-1")
	if err != nil {
		t.Fatalf("SaveEtcdSnapshot() error = %v", err)
	}
	wantRef := snapshotDir + "/operation-1.db"
	if ref != wantRef {
		t.Fatalf("snapshot ref = %q", ref)
	}
	if err := runtime.VerifyEtcdSnapshot(t.Context(), ref); err != nil {
		t.Fatalf("VerifyEtcdSnapshot() error = %v", err)
	}
	want := []commandCall{
		{name: "etcdctl", args: []string{
			"--endpoints=https://127.0.0.1:2379",
			"--cacert=/etc/kubernetes/pki/etcd/ca.crt",
			"--cert=/etc/kubernetes/pki/etcd/server.crt",
			"--key=/etc/kubernetes/pki/etcd/server.key",
			"snapshot",
			"save",
			wantRef,
		}},
		{name: "etcdutl", args: []string{"snapshot", "status", wantRef}},
	}
	if !equalCommandCalls(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestRuntimeはNodeHealthをkubectlから観測する(t *testing.T) {
	runner := &recordingCommandRunner{
		outputs: []string{
			"True\n",
			"v1.36.0\n",
		},
	}
	runtime := NewRuntimeForTest(RuntimeConfig{KubectlPath: "kubectl", NodeName: "node-a"}, runner)

	health, err := runtime.ObserveHealth(t.Context(), domain.Plan{NodeRole: domain.NodeRoleWorker})
	if err != nil {
		t.Fatalf("ObserveHealth() error = %v", err)
	}
	if !health.NodeReady || health.NodeVersion != "v1.36.0" {
		t.Fatalf("health = %#v", health)
	}
	want := []commandCall{
		{name: "kubectl", args: []string{"get", "node", "node-a", "-o=jsonpath={.status.conditions[?(@.type==\"Ready\")].status}"}},
		{name: "kubectl", args: []string{"get", "node", "node-a", "-o=jsonpath={.status.nodeInfo.kubeletVersion}"}},
	}
	if !equalCommandCalls(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestRuntimeはControlPlaneHealthを個別観測する(t *testing.T) {
	runner := &recordingCommandRunner{
		outputs: []string{
			"True\n",
			"v1.36.0\n",
			"true\n",
			"true\n",
			"true\n",
			"true\n",
			"https://127.0.0.1:2379 is healthy: successfully committed proposal: took = 2.1ms\n",
			"ok",
		},
	}
	runtime := NewRuntimeForTest(RuntimeConfig{
		KubectlPath: "kubectl",
		EtcdctlPath: "etcdctl",
		NodeName:    "cp-1",
	}, runner)

	health, err := runtime.ObserveHealth(t.Context(), domain.Plan{NodeRole: domain.NodeRoleControlPlane})
	if err != nil {
		t.Fatalf("ObserveHealth() error = %v", err)
	}
	if !health.NodeReady || health.NodeVersion != "v1.36.0" {
		t.Fatalf("health = %#v", health)
	}
	if !health.StaticPodsReady || !health.EtcdQuorum || !health.APIHealthy {
		t.Fatalf("health = %#v", health)
	}
	want := []commandCall{
		{name: "kubectl", args: []string{"get", "node", "cp-1", "-o=jsonpath={.status.conditions[?(@.type==\"Ready\")].status}"}},
		{name: "kubectl", args: []string{"get", "node", "cp-1", "-o=jsonpath={.status.nodeInfo.kubeletVersion}"}},
		{name: "kubectl", args: []string{"-n", "kube-system", "get", "pod", "kube-apiserver-cp-1", "-o=jsonpath={.status.containerStatuses[*].ready}"}},
		{name: "kubectl", args: []string{"-n", "kube-system", "get", "pod", "kube-controller-manager-cp-1", "-o=jsonpath={.status.containerStatuses[*].ready}"}},
		{name: "kubectl", args: []string{"-n", "kube-system", "get", "pod", "kube-scheduler-cp-1", "-o=jsonpath={.status.containerStatuses[*].ready}"}},
		{name: "kubectl", args: []string{"-n", "kube-system", "get", "pod", "etcd-cp-1", "-o=jsonpath={.status.containerStatuses[*].ready}"}},
		{name: "etcdctl", args: []string{
			"--endpoints=https://127.0.0.1:2379",
			"--cacert=/etc/kubernetes/pki/etcd/ca.crt",
			"--cert=/etc/kubernetes/pki/etcd/server.crt",
			"--key=/etc/kubernetes/pki/etcd/server.key",
			"endpoint",
			"health",
			"--cluster",
		}},
		{name: "kubectl", args: []string{"get", "--raw=/readyz"}},
	}
	if !equalCommandCalls(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestRuntimeはControlPlaneStaticPod未準備を検出する(t *testing.T) {
	runner := &recordingCommandRunner{
		outputs: []string{
			"True\n",
			"v1.36.0\n",
			"true\n",
			"false\n",
		},
	}
	runtime := NewRuntimeForTest(RuntimeConfig{KubectlPath: "kubectl", NodeName: "cp-1"}, runner)

	health, err := runtime.ObserveHealth(t.Context(), domain.Plan{NodeRole: domain.NodeRoleControlPlane})
	if err != nil {
		t.Fatalf("ObserveHealth() error = %v", err)
	}
	if health.StaticPodsReady {
		t.Fatalf("health = %#v", health)
	}
	if health.EtcdQuorum || health.APIHealthy {
		t.Fatalf("health = %#v", health)
	}
}

type commandCall struct {
	name string
	args []string
}

type recordingCommandRunner struct {
	calls   []commandCall
	outputs []string
}

func (runner *recordingCommandRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	runner.calls = append(runner.calls, commandCall{name: name, args: append([]string(nil), args...)})
	if len(runner.outputs) == 0 {
		return "", nil
	}
	output := runner.outputs[0]
	runner.outputs = runner.outputs[1:]
	return output, nil
}

func equalCommandCalls(left, right []commandCall) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].name != right[index].name {
			return false
		}
		if len(left[index].args) != len(right[index].args) {
			return false
		}
		for argIndex := range left[index].args {
			if left[index].args[argIndex] != right[index].args[argIndex] {
				return false
			}
		}
	}
	return true
}
