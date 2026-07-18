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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/node/entity/nodelifecycleengine"
)

const (
	defaultKubeadmPath = "kubeadm"
	defaultEtcdctlPath = "etcdctl"
	defaultEtcdutlPath = "etcdutl"
	defaultKubectlPath = "kubectl"
	defaultSnapshotDir = "/var/lib/tart/snapshots"
)

type RuntimeConfig struct {
	KubeadmPath string
	EtcdctlPath string
	EtcdutlPath string
	KubectlPath string
	SnapshotDir string
	NodeName    string
}

type CommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type LocalRuntime struct {
	config RuntimeConfig
	runner CommandRunner
}

func NewRuntime(config RuntimeConfig) *LocalRuntime {
	return NewRuntimeForTest(config, commandRunner{})
}

func NewRuntimeForTest(config RuntimeConfig, runner CommandRunner) *LocalRuntime {
	if config.KubeadmPath == "" {
		config.KubeadmPath = defaultKubeadmPath
	}
	if config.EtcdctlPath == "" {
		config.EtcdctlPath = defaultEtcdctlPath
	}
	if config.EtcdutlPath == "" {
		config.EtcdutlPath = defaultEtcdutlPath
	}
	if config.KubectlPath == "" {
		config.KubectlPath = defaultKubectlPath
	}
	if config.SnapshotDir == "" {
		config.SnapshotDir = defaultSnapshotDir
	}
	return &LocalRuntime{config: config, runner: runner}
}

func (runtime *LocalRuntime) UpgradePlan(ctx context.Context, targetVersion string) error {
	_, err := runtime.runner.Run(ctx, runtime.config.KubeadmPath, "upgrade", "plan", targetVersion)
	return err
}

func (runtime *LocalRuntime) SaveEtcdSnapshot(ctx context.Context, operationID string) (string, error) {
	if err := os.MkdirAll(runtime.config.SnapshotDir, 0o700); err != nil {
		return "", fmt.Errorf("create snapshot directory: %w", err)
	}
	ref := filepath.Join(runtime.config.SnapshotDir, operationID+".db")
	_, err := runtime.runner.Run(ctx, runtime.config.EtcdctlPath, append(etcdEndpointArgs(), "snapshot", "save", ref)...)
	if err != nil {
		return "", err
	}
	return ref, nil
}

func (runtime *LocalRuntime) VerifyEtcdSnapshot(ctx context.Context, snapshotRef string) error {
	_, err := runtime.runner.Run(ctx, runtime.config.EtcdutlPath, "snapshot", "status", snapshotRef)
	return err
}

func (runtime *LocalRuntime) UpgradeApply(ctx context.Context, targetVersion string) error {
	_, err := runtime.runner.Run(ctx, runtime.config.KubeadmPath, "upgrade", "apply", "-y", targetVersion)
	return err
}

func (runtime *LocalRuntime) UpgradeNode(ctx context.Context, targetVersion string) error {
	_, err := runtime.runner.Run(ctx, runtime.config.KubeadmPath, "upgrade", "node")
	return err
}

func (runtime *LocalRuntime) ObserveHealth(ctx context.Context, plan domain.Plan) (domain.HealthInput, error) {
	if runtime.config.NodeName == "" {
		return domain.HealthInput{}, fmt.Errorf("node name is required for health verification")
	}
	health, err := runtime.observeNodeHealth(ctx, plan.NodeRole)
	if err != nil {
		return domain.HealthInput{}, err
	}
	if plan.NodeRole != domain.NodeRoleControlPlane {
		return health, nil
	}

	staticPodsReady, err := runtime.observeStaticPodsReady(ctx)
	if err != nil {
		return domain.HealthInput{}, err
	}
	health.StaticPodsReady = staticPodsReady

	etcdQuorum, err := runtime.observeEtcdQuorum(ctx)
	if err != nil {
		return domain.HealthInput{}, err
	}
	health.EtcdQuorum = etcdQuorum

	apiHealthy, err := runtime.observeAPIHealth(ctx)
	if err != nil {
		return domain.HealthInput{}, err
	}
	health.APIHealthy = apiHealthy
	return health, nil
}

func etcdEndpointArgs() []string {
	return []string{
		"--endpoints=https://127.0.0.1:2379",
		"--cacert=/etc/kubernetes/pki/etcd/ca.crt",
		"--cert=/etc/kubernetes/pki/etcd/server.crt",
		"--key=/etc/kubernetes/pki/etcd/server.key",
	}
}

func (runtime *LocalRuntime) observeNodeHealth(ctx context.Context, nodeRole domain.NodeRole) (domain.HealthInput, error) {
	ready, err := runtime.runner.Run(
		ctx,
		runtime.config.KubectlPath,
		"get",
		"node",
		runtime.config.NodeName,
		"-o=jsonpath={.status.conditions[?(@.type==\"Ready\")].status}",
	)
	if err != nil {
		return domain.HealthInput{}, err
	}
	version, err := runtime.runner.Run(
		ctx,
		runtime.config.KubectlPath,
		"get",
		"node",
		runtime.config.NodeName,
		"-o=jsonpath={.status.nodeInfo.kubeletVersion}",
	)
	if err != nil {
		return domain.HealthInput{}, err
	}
	return domain.HealthInput{
		NodeReady:   strings.TrimSpace(ready) == "True",
		NodeVersion: strings.TrimSpace(version),
		NodeRole:    nodeRole,
	}, nil
}

func (runtime *LocalRuntime) observeStaticPodsReady(ctx context.Context) (bool, error) {
	components := []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler", "etcd"}
	for _, component := range components {
		output, err := runtime.runner.Run(
			ctx,
			runtime.config.KubectlPath,
			"-n",
			"kube-system",
			"get",
			"pod",
			component+"-"+runtime.config.NodeName,
			"-o=jsonpath={.status.containerStatuses[*].ready}",
		)
		if err != nil {
			return false, err
		}
		if !allReady(output) {
			return false, nil
		}
	}
	return true, nil
}

func (runtime *LocalRuntime) observeEtcdQuorum(ctx context.Context) (bool, error) {
	output, err := runtime.runner.Run(
		ctx,
		runtime.config.EtcdctlPath,
		append(etcdEndpointArgs(), "endpoint", "health", "--cluster")...,
	)
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(output)), "healthy"), nil
}

func (runtime *LocalRuntime) observeAPIHealth(ctx context.Context) (bool, error) {
	output, err := runtime.runner.Run(
		ctx,
		runtime.config.KubectlPath,
		"get",
		"--raw=/readyz",
	)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(output), "ok"), nil
}

func allReady(output string) bool {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if field != "true" {
			return false
		}
	}
	return true
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
