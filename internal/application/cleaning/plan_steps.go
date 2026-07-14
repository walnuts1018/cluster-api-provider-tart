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

package cleaning

import (
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func buildSignedCleaningPlanStep(
	host *infrastructurev1beta1.TartHost,
	policy infrastructurev1beta1.DeletionPolicy,
	operation *infrastructurev1beta1.TartHostOperation,
	signer PlanSigner,
) (SignedCleaningPlan, error) {
	plan, err := BuildCleaningPlan(CleaningPlanInput{
		OperationID:    operation.Spec.OperationID,
		Host:           host,
		DeletionPolicy: policy,
		Deadline:       operation.Spec.Deadline.Time,
	}, signer.KeyID, signer.PrivateKey)
	if err != nil {
		return SignedCleaningPlan{}, fmt.Errorf("build signed Cleaning Plan: %w", err)
	}
	return plan, nil
}
