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

import "reflect"

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

type MachineSpecSnapshot struct {
	Version string
	Spec    any
}

type TartMachineSpecSnapshot struct {
	ImageRef        string
	UpdatePolicy    any
	PlatformProfile string
	HostSelector    any
	ProviderID      string
	DeletionPolicy  string
}

// ChangeSetはCAPI Runtime Hookが渡すcurrentとdesiredの更新判定入力を正規化した値である。
type ChangeSet struct {
	CurrentMachine         MachineSpecSnapshot
	DesiredMachine         MachineSpecSnapshot
	CurrentTartMachine     TartMachineSpecSnapshot
	DesiredTartMachine     TartMachineSpecSnapshot
	CurrentBootstrapConfig any
	DesiredBootstrapConfig any
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
func Classify(changes ChangeSet) Classification {
	classification := Classification{}

	classifyMachine(&classification, changes.CurrentMachine, changes.DesiredMachine)
	classifyBootstrap(&classification, changes.CurrentBootstrapConfig, changes.DesiredBootstrapConfig)
	classifyTartMachine(&classification, changes.CurrentTartMachine, changes.DesiredTartMachine)

	return classification
}

func classifyMachine(
	classification *Classification,
	current MachineSpecSnapshot,
	desired MachineSpecSnapshot,
) {
	if current.Version != desired.Version {
		classification.reject(FieldMachineVersion)
	}
	current.Version = desired.Version
	if !reflect.DeepEqual(current.Spec, desired.Spec) {
		classification.reject(FieldMachineSpec)
	}
}

func classifyBootstrap(classification *Classification, current any, desired any) {
	if !reflect.DeepEqual(current, desired) {
		classification.reject(FieldBootstrapConfig)
	}
}

func classifyTartMachine(
	classification *Classification,
	current TartMachineSpecSnapshot,
	desired TartMachineSpecSnapshot,
) {
	if current.ImageRef != desired.ImageRef {
		classification.allow(FieldTartMachineImageRef)
	}
	if !reflect.DeepEqual(current.UpdatePolicy, desired.UpdatePolicy) {
		classification.allow(FieldTartMachineUpdatePolicy)
	}
	if current.PlatformProfile != desired.PlatformProfile {
		classification.reject(FieldTartMachinePlatformProfile)
	}
	if !reflect.DeepEqual(current.HostSelector, desired.HostSelector) {
		classification.reject(FieldTartMachineHostSelector)
	}
	if current.ProviderID != desired.ProviderID {
		classification.reject(FieldTartMachineProviderID)
	}
	if current.DeletionPolicy != desired.DeletionPolicy {
		classification.reject(FieldTartMachineDeletionPolicy)
	}
}

func (classification *Classification) allow(path FieldPath) {
	classification.Changed = append(classification.Changed, path)
	classification.Allowed = append(classification.Allowed, path)
}

func (classification *Classification) reject(path FieldPath) {
	classification.Changed = append(classification.Changed, path)
	classification.Rejected = append(classification.Rejected, path)
}
