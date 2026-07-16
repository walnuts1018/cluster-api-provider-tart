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
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/inplaceupdate"
)

type ClassificationInput struct {
	CurrentMachine         clusterv1.Machine
	DesiredMachine         clusterv1.Machine
	CurrentTartMachine     infrastructurev1beta1.TartMachine
	DesiredTartMachine     infrastructurev1beta1.TartMachine
	CurrentBootstrapConfig runtime.RawExtension
	DesiredBootstrapConfig runtime.RawExtension
}

func ClassifyChanges(input ClassificationInput) (domain.EligibilityDecision, error) {
	changes, err := mapChangeSet(input)
	if err != nil {
		return nil, err
	}
	return domain.Decide(changes), nil
}

func mapChangeSet(input ClassificationInput) (domain.ChangeSet, error) {
	currentBootstrap, err := bootstrapSpecSnapshot(input.CurrentBootstrapConfig)
	if err != nil {
		return domain.ChangeSet{}, fmt.Errorf("decode current BootstrapConfig: %w", err)
	}
	desiredBootstrap, err := bootstrapSpecSnapshot(input.DesiredBootstrapConfig)
	if err != nil {
		return domain.ChangeSet{}, fmt.Errorf("decode desired BootstrapConfig: %w", err)
	}

	currentMachineSpec := input.CurrentMachine.Spec.DeepCopy()
	desiredMachineSpec := input.DesiredMachine.Spec.DeepCopy()
	currentMachineSpec.Version = ""
	desiredMachineSpec.Version = ""

	return domain.ChangeSet{
		CurrentMachine: domain.MachineSpecSnapshot{
			Version: input.CurrentMachine.Spec.Version,
			Spec:    currentMachineSpec,
		},
		DesiredMachine: domain.MachineSpecSnapshot{
			Version: input.DesiredMachine.Spec.Version,
			Spec:    desiredMachineSpec,
		},
		CurrentTartMachine: domain.TartMachineSpecSnapshot{
			ImageRef:        input.CurrentTartMachine.Spec.Image.Ref,
			UpdatePolicy:    input.CurrentTartMachine.Spec.UpdatePolicy.DeepCopy(),
			PlatformProfile: input.CurrentTartMachine.Spec.PlatformProfile,
			HostSelector:    deepCopyHostSelector(input.CurrentTartMachine.Spec.HostSelector),
			ProviderID:      input.CurrentTartMachine.Spec.ProviderID,
			DeletionPolicy:  string(input.CurrentTartMachine.Spec.DeletionPolicy),
		},
		DesiredTartMachine: domain.TartMachineSpecSnapshot{
			ImageRef:        input.DesiredTartMachine.Spec.Image.Ref,
			UpdatePolicy:    input.DesiredTartMachine.Spec.UpdatePolicy.DeepCopy(),
			PlatformProfile: input.DesiredTartMachine.Spec.PlatformProfile,
			HostSelector:    deepCopyHostSelector(input.DesiredTartMachine.Spec.HostSelector),
			ProviderID:      input.DesiredTartMachine.Spec.ProviderID,
			DeletionPolicy:  string(input.DesiredTartMachine.Spec.DeletionPolicy),
		},
		CurrentBootstrapConfig: currentBootstrap,
		DesiredBootstrapConfig: desiredBootstrap,
	}, nil
}

func bootstrapSpecSnapshot(extension runtime.RawExtension) (any, error) {
	if len(extension.Raw) == 0 && extension.Object == nil {
		return map[string]any{}, nil
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

func deepCopyHostSelector(selector infrastructurev1beta1.HostSelector) infrastructurev1beta1.HostSelector {
	if apiequality.Semantic.DeepEqual(selector, infrastructurev1beta1.HostSelector{}) {
		return infrastructurev1beta1.HostSelector{}
	}
	return *selector.DeepCopy()
}
