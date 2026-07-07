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
	"encoding/json"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

// FieldPathはインプレース更新判定で識別する閉じた差分種別である。
type FieldPath string

const (
	FieldMachineVersion             FieldPath = "Machine.spec.version"
	FieldMachineSpec                FieldPath = "Machine.spec"
	FieldBootstrapConfig            FieldPath = "BootstrapConfig.spec"
	FieldTartMachineImageRef        FieldPath = "TartMachine.spec.image.ref"
	FieldTartMachineUpdatePolicy    FieldPath = "TartMachine.spec.updatePolicy"
	FieldTartMachinePlatformProfile FieldPath = "TartMachine.spec.platformProfile"
	FieldTartMachineHostSelector    FieldPath = "TartMachine.spec.hostSelector"
	FieldTartMachineProviderID      FieldPath = "TartMachine.spec.providerID"
	FieldTartMachineDeletionPolicy  FieldPath = "TartMachine.spec.deletionPolicy"
)

// ChangeSetはCAPI Runtime Hookが渡すcurrentとdesiredの更新判定入力である。
type ChangeSet struct {
	CurrentMachine         clusterv1.Machine
	DesiredMachine         clusterv1.Machine
	CurrentTartMachine     infrastructurev1beta1.TartMachine
	DesiredTartMachine     infrastructurev1beta1.TartMachine
	CurrentBootstrapConfig runtime.RawExtension
	DesiredBootstrapConfig runtime.RawExtension
}

// Classificationは検出した全差分を許可差分と拒否差分へ分類した結果である。
type Classification struct {
	Changed  []FieldPath
	Allowed  []FieldPath
	Rejected []FieldPath
}

// CanUpdateInPlaceは少なくとも1つのOSOnly差分があり、拒否差分がない場合だけtrueを返す。
func (classification Classification) CanUpdateInPlace() bool {
	return len(classification.Changed) > 0 && len(classification.Rejected) == 0
}

// ClassifyはOSOnly更新で扱える差分を副作用なしで分類する。
func Classify(changes ChangeSet) (Classification, error) {
	classification := Classification{}

	classifyMachine(&classification, changes.CurrentMachine.Spec, changes.DesiredMachine.Spec)
	if err := classifyBootstrap(
		&classification,
		changes.CurrentBootstrapConfig,
		changes.DesiredBootstrapConfig,
	); err != nil {
		return Classification{}, err
	}
	classifyTartMachine(
		&classification,
		changes.CurrentTartMachine.Spec,
		changes.DesiredTartMachine.Spec,
	)

	return classification, nil
}

func classifyMachine(
	classification *Classification,
	current clusterv1.MachineSpec,
	desired clusterv1.MachineSpec,
) {
	if current.Version != desired.Version {
		classification.reject(FieldMachineVersion)
		current.Version = desired.Version
	}
	if !apiequality.Semantic.DeepEqual(current, desired) {
		classification.reject(FieldMachineSpec)
	}
}

func classifyBootstrap(
	classification *Classification,
	current runtime.RawExtension,
	desired runtime.RawExtension,
) error {
	currentSpec, err := rawSpec(current)
	if err != nil {
		return fmt.Errorf("decode current BootstrapConfig: %w", err)
	}
	desiredSpec, err := rawSpec(desired)
	if err != nil {
		return fmt.Errorf("decode desired BootstrapConfig: %w", err)
	}
	if !apiequality.Semantic.DeepEqual(currentSpec, desiredSpec) {
		classification.reject(FieldBootstrapConfig)
	}
	return nil
}

func classifyTartMachine(
	classification *Classification,
	current infrastructurev1beta1.TartMachineSpec,
	desired infrastructurev1beta1.TartMachineSpec,
) {
	if current.Image.Ref != desired.Image.Ref {
		classification.allow(FieldTartMachineImageRef)
	}
	if !apiequality.Semantic.DeepEqual(current.UpdatePolicy, desired.UpdatePolicy) {
		classification.allow(FieldTartMachineUpdatePolicy)
	}
	if current.PlatformProfile != desired.PlatformProfile {
		classification.reject(FieldTartMachinePlatformProfile)
	}
	if !apiequality.Semantic.DeepEqual(current.HostSelector, desired.HostSelector) {
		classification.reject(FieldTartMachineHostSelector)
	}
	if current.ProviderID != desired.ProviderID {
		classification.reject(FieldTartMachineProviderID)
	}
	if current.DeletionPolicy != desired.DeletionPolicy {
		classification.reject(FieldTartMachineDeletionPolicy)
	}
}

func rawSpec(extension runtime.RawExtension) (any, error) {
	if len(extension.Raw) == 0 && extension.Object == nil {
		return nil, nil
	}

	raw := extension.Raw
	if len(raw) == 0 {
		var err error
		raw, err = json.Marshal(extension.Object)
		if err != nil {
			return nil, fmt.Errorf("marshal object: %w", err)
		}
	}

	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}
	return object["spec"], nil
}

func (classification *Classification) allow(path FieldPath) {
	classification.Changed = append(classification.Changed, path)
	classification.Allowed = append(classification.Allowed, path)
}

func (classification *Classification) reject(path FieldPath) {
	classification.Changed = append(classification.Changed, path)
	classification.Rejected = append(classification.Rejected, path)
}
