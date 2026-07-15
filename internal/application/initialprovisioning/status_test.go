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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestStatusWithProvisionedKeepsObservedMachineIDOnceSet(t *testing.T) {
	t.Parallel()

	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Generation: 5},
		Status: infrastructurev1beta1.TartMachineStatus{
			InstalledMachineID: "machine-id-a",
		},
	}

	status := StatusWithProvisioned(machine, nil, "machine-id-b", "v1.35.0")
	if status.InstalledMachineID != "machine-id-a" {
		t.Fatalf("InstalledMachineID = %q, want %q", status.InstalledMachineID, "machine-id-a")
	}

	machine.Status.InstalledMachineID = ""
	status = StatusWithProvisioned(machine, nil, "machine-id-b", "v1.35.0")
	if status.InstalledMachineID != "machine-id-b" {
		t.Fatalf("InstalledMachineID = %q, want %q", status.InstalledMachineID, "machine-id-b")
	}
}
