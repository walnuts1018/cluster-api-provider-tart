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

package step

import (
	"encoding/json"
	"fmt"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

const defaultCleaningDeadline = 30 * time.Minute

func BuildOperationDraft(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	planDigest string,
	now time.Time,
) (*infrastructurev1beta1.TartHostOperation, error) {
	desiredObjectsDigest, err := cleaningObjectsDigest(machine, host)
	if err != nil {
		return nil, fmt.Errorf("build cleaning desired objects digest: %w", err)
	}
	operationID, err := deterministicOperationID(host, machine, machine.Spec.DeletionPolicy)
	if err != nil {
		return nil, err
	}
	operationType := infrastructurev1beta1.OperationTypeClean
	deadline := now.Add(defaultCleaningDeadline)
	if machine.Spec.DeletionPolicy == infrastructurev1beta1.DeletionPolicyWipeAll {
		operationType = infrastructurev1beta1.OperationTypeWipeAll
		deadline = now.Add(WipeAllDeadline(observedRootDiskSize(host)))
	}
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: machine.Namespace,
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID:          operationID,
			Type:                 operationType,
			PlanDigest:           planDigest,
			DesiredObjectsDigest: desiredObjectsDigest,
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
			Deadline: metav1.NewTime(deadline),
		},
	}, nil
}

func observedRootDiskSize(host *infrastructurev1beta1.TartHost) int64 {
	if host.Status.Inventory.RootDisk.SizeBytes > 0 {
		return host.Status.Inventory.RootDisk.SizeBytes
	}
	return host.Spec.RootDeviceHints.MinSizeBytes
}

func deterministicOperationID(
	host *infrastructurev1beta1.TartHost,
	machine *infrastructurev1beta1.TartMachine,
	policy infrastructurev1beta1.DeletionPolicy,
) (string, error) {
	key := "clean/" + string(policy) + "/" + string(host.UID) + "/" + string(machine.UID)
	id, err := operationdomain.DeterministicID(key)
	if err != nil {
		return "", fmt.Errorf("generate deterministic Cleaning operation ID: %w", err)
	}
	return id.String(), nil
}

func cleaningObjectsDigest(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) (string, error) {
	input := struct {
		MachineUID      string                                `json:"machineUID"`
		HostUID         string                                `json:"hostUID"`
		DeletionPolicy  infrastructurev1beta1.DeletionPolicy  `json:"deletionPolicy"`
		RootDeviceHints infrastructurev1beta1.RootDeviceHints `json:"rootDeviceHints"`
	}{
		MachineUID:      string(machine.UID),
		HostUID:         string(host.UID),
		DeletionPolicy:  machine.Spec.DeletionPolicy,
		RootDeviceHints: host.Spec.RootDeviceHints,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal Cleaning desired state: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(encoded)
	if err != nil {
		return "", fmt.Errorf("canonicalize Cleaning desired state: %w", err)
	}
	return digest.FromBytes(canonical).String(), nil
}
