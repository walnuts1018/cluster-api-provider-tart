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
	"testing"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestStatusWithUpdateSucceededはCommit済みImageとDistributionVersionを反映する(t *testing.T) {
	status := StatusWithUpdateSucceeded(
		&infrastructurev1beta1.TartMachine{},
		&infrastructurev1beta1.TartHostOperation{
			Spec: infrastructurev1beta1.TartHostOperationSpec{
				TargetSlot:                infrastructurev1beta1.OSSlotB,
				TargetImageDigest:         "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				TargetDistributionVersion: "v1.35.0",
			},
		},
	)

	if status.ActiveSlot != infrastructurev1beta1.OSSlotB ||
		status.InstalledImageDigest != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		status.InstalledDistributionVersion != "v1.35.0" {
		t.Fatalf("status = %#v, want committed slot, image digest, and distribution version", status)
	}
}
