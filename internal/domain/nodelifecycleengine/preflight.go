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
	"strconv"
	"strings"
)

// UpdateClassはNode Lifecycle Engineが扱う更新分類である。
type UpdateClass string

const (
	UpdateClassKubernetesBinary UpdateClass = "KubernetesBinary"
	UpdateClassStateMigration   UpdateClass = "StateMigration"
)

// LifecycleRuntimeはNode Lifecycle Serviceが実行するruntime更新engineを識別する。
type LifecycleRuntime string

const (
	LifecycleRuntimeKubeadm     LifecycleRuntime = "kubeadm.cluster.x-k8s.io/v1"
	LifecycleRuntimeK0s         LifecycleRuntime = "k0sproject.io/k0s/v1"
	LifecycleRuntimeUnsupported LifecycleRuntime = "unsupported"
)

// NodeRoleはLifecycle Planの対象Node種別である。
type NodeRole string

const (
	NodeRoleWorker       NodeRole = "Worker"
	NodeRoleControlPlane NodeRole = "ControlPlane"
)

// PreflightInputはNode Lifecycle Engine Plan開始前の純粋判定入力である。
type PreflightInput struct {
	LifecycleRuntime LifecycleRuntime
	CurrentVersion   string
	TargetVersion    string
	UpdateClass      UpdateClass
	NodeRole         NodeRole

	ControlPlaneAcceptedVersion     string
	RequireControlPlaneTargetAccept bool
	SnapshotRef                     string
}

// PreflightはdiskやKubernetes APIへ触れる前に、Lifecycle Planを開始可能か判定する。
func Preflight(input PreflightInput) error {
	if failure := preflightFailure(input); failure != nil {
		return fmt.Errorf("node lifecycle engine preflight rejected: %T", failure)
	}
	return nil
}

type kubernetesVersion struct {
	major int
	minor int
	patch int
}

func parseKubernetesVersion(value string) (kubernetesVersion, error) {
	version := strings.TrimPrefix(value, "v")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return kubernetesVersion{}, fmt.Errorf("version must be vMAJOR.MINOR.PATCH")
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return kubernetesVersion{}, fmt.Errorf("major must be numeric")
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return kubernetesVersion{}, fmt.Errorf("minor must be numeric")
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return kubernetesVersion{}, fmt.Errorf("patch must be numeric")
	}
	return kubernetesVersion{major: major, minor: minor, patch: patch}, nil
}

func preflightFailure(input PreflightInput) Failure {
	if input.LifecycleRuntime != LifecycleRuntimeKubeadm &&
		input.LifecycleRuntime != LifecycleRuntimeK0s &&
		input.LifecycleRuntime != LifecycleRuntimeUnsupported {
		return UnsupportedLifecycleRuntime{Value: string(input.LifecycleRuntime)}
	}
	if failure := validateNodeLifecycleSupport(input); failure != nil {
		return failure
	}
	current, err := parseKubernetesVersion(input.CurrentVersion)
	if err != nil {
		return InvalidCurrentVersion{Value: input.CurrentVersion}
	}
	target, err := parseKubernetesVersion(input.TargetVersion)
	if err != nil {
		return InvalidTargetVersion{Value: input.TargetVersion}
	}
	if failure := validateForwardMinorStep(current, target); failure != nil {
		return failure
	}
	if failure := validateWorkerOrdering(input); failure != nil {
		return failure
	}
	if input.UpdateClass == UpdateClassStateMigration && input.SnapshotRef == "" {
		return SnapshotRequired{Step: StepDistributionApplied}
	}
	return nil
}

func validateNodeLifecycleSupport(input PreflightInput) Failure {
	if input.LifecycleRuntime == LifecycleRuntimeUnsupported {
		return LifecycleRuntimeUnsupportedFailure{
			LifecycleRuntime: input.LifecycleRuntime,
			UpdateClass:      input.UpdateClass,
		}
	}
	return nil
}

func validateForwardMinorStep(current kubernetesVersion, target kubernetesVersion) Failure {
	if target.major != current.major {
		return MajorVersionChangeUnsupported{}
	}
	if target.minor < current.minor {
		return VersionDowngradeUnsupported{}
	}
	if target.minor-current.minor > 1 {
		return MinorVersionSkipUnsupported{}
	}
	if target.minor == current.minor && target.patch < current.patch {
		return VersionDowngradeUnsupported{}
	}
	return nil
}

func validateWorkerOrdering(input PreflightInput) Failure {
	if input.NodeRole != NodeRoleWorker || !input.RequireControlPlaneTargetAccept {
		return nil
	}
	if input.ControlPlaneAcceptedVersion != input.TargetVersion {
		return WorkerControlPlaneOrderUnsatisfied{
			AcceptedVersion: input.ControlPlaneAcceptedVersion,
			TargetVersion:   input.TargetVersion,
		}
	}
	return nil
}
