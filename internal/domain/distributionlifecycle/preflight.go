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

import (
	"fmt"
	"strconv"
	"strings"
)

// UpdateClassはDistribution Lifecycleが扱う更新分類である。
type UpdateClass string

const (
	UpdateClassKubernetesBinary UpdateClass = "KubernetesBinary"
	UpdateClassStateMigration   UpdateClass = "StateMigration"
)

// NodeRoleはLifecycle Planの対象Node種別である。
type NodeRole string

const (
	NodeRoleWorker       NodeRole = "Worker"
	NodeRoleControlPlane NodeRole = "ControlPlane"
)

// PreflightInputはDistribution Lifecycle Plan開始前の純粋判定入力である。
type PreflightInput struct {
	CurrentVersion string
	TargetVersion  string
	UpdateClass    UpdateClass
	NodeRole       NodeRole

	ControlPlaneAcceptedVersion     string
	RequireControlPlaneTargetAccept bool
	SnapshotRef                     string
}

// PreflightはdiskやKubernetes APIへ触れる前に、Lifecycle Planを開始可能か判定する。
func Preflight(input PreflightInput) error {
	current, err := parseKubernetesVersion(input.CurrentVersion)
	if err != nil {
		return fmt.Errorf("parse current Kubernetes version: %w", err)
	}
	target, err := parseKubernetesVersion(input.TargetVersion)
	if err != nil {
		return fmt.Errorf("parse target Kubernetes version: %w", err)
	}
	if err := validateForwardMinorStep(current, target); err != nil {
		return err
	}
	if err := validateWorkerOrdering(input); err != nil {
		return err
	}
	if input.UpdateClass == UpdateClassStateMigration && input.SnapshotRef == "" {
		return fmt.Errorf("StateMigration requires SnapshotRef before applying lifecycle steps")
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

func validateForwardMinorStep(current kubernetesVersion, target kubernetesVersion) error {
	if target.major != current.major {
		return fmt.Errorf("Kubernetes major version changes are not supported")
	}
	if target.minor < current.minor {
		return fmt.Errorf("Kubernetes version downgrade is not supported")
	}
	if target.minor-current.minor > 1 {
		return fmt.Errorf("Kubernetes minor version cannot skip more than one minor")
	}
	if target.minor == current.minor && target.patch < current.patch {
		return fmt.Errorf("Kubernetes patch version downgrade is not supported")
	}
	return nil
}

func validateWorkerOrdering(input PreflightInput) error {
	if input.NodeRole != NodeRoleWorker || !input.RequireControlPlaneTargetAccept {
		return nil
	}
	if input.ControlPlaneAcceptedVersion != input.TargetVersion {
		return fmt.Errorf("worker lifecycle update requires control plane to accept target version first")
	}
	return nil
}
