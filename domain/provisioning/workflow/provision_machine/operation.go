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

package initialprovisioning

import (
	"encoding/json"
	"fmt"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

const defaultOperationDeadline = 2 * time.Hour

func buildOperationDraft(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	planDigest string,
) (*infrastructurev1beta1.TartHostOperation, error) {
	deadline := metav1.NewTime(time.Now().UTC().Truncate(time.Second).Add(defaultOperationDeadline))
	return buildOperationDraftWithDeadline(machine, host, planDigest, deadline)
}

func buildOperationDraftWithDeadline(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	planDigest string,
	deadline metav1.Time,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operationUID, err := deterministicOperationUID(host, machine)
	if err != nil {
		return nil, err
	}

	desiredMachine := machine.DeepCopy()
	expectedProviderID := fmt.Sprintf("tart://%s", host.Name)
	if desiredMachine.Spec.ProviderID != "" && desiredMachine.Spec.ProviderID != expectedProviderID {
		return nil, fmt.Errorf(
			"TartMachine providerID %q does not match reserved TartHost %q",
			desiredMachine.Spec.ProviderID,
			host.Name,
		)
	}
	desiredMachine.Spec.ProviderID = expectedProviderID
	objectsDigest, err := DesiredObjectsDigest(desiredMachine)
	if err != nil {
		return nil, fmt.Errorf("build desired objects digest: %w", err)
	}
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: machine.Namespace,
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: operationUID,
			Type:        infrastructurev1beta1.OperationTypeProvision,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: host.Namespace,
				Name:      host.Name,
				UID:       host.UID,
			},
			MachineRef: &infrastructurev1beta1.ResourceReference{
				Namespace: machine.Namespace,
				Name:      machine.Name,
				UID:       machine.UID,
			},
			PlanDigest:           planDigest,
			DesiredObjectsDigest: objectsDigest,
			Deadline:             deadline,
		},
	}, nil
}

func DesiredObjectsDigest(machine *infrastructurev1beta1.TartMachine) (string, error) {
	input := struct {
		MachineUID string                                `json:"machineUID"`
		Spec       infrastructurev1beta1.TartMachineSpec `json:"spec"`
	}{
		MachineUID: string(machine.UID),
		Spec:       machine.Spec,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal TartMachine desired state: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(encoded)
	if err != nil {
		return "", fmt.Errorf("canonicalize TartMachine desired state: %w", err)
	}
	return digest.FromBytes(canonical).String(), nil
}

func deterministicOperationUID(host *infrastructurev1beta1.TartHost, machine *infrastructurev1beta1.TartMachine) (string, error) {
	key := string(host.UID) + "/" + string(machine.UID)
	id, err := operationdomain.DeterministicID(key)
	if err != nil {
		return "", fmt.Errorf("generate deterministic operation ID: %w", err)
	}
	return id.String(), nil
}
