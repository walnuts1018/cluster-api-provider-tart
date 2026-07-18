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
	"crypto/ed25519"

	cleaningstep "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/cleaning"
)

type CleaningPlanInput = cleaningstep.CleaningPlanInput
type SignedCleaningPlan = cleaningstep.SignedCleaningPlan

func BuildCleaningPlan(
	input CleaningPlanInput,
	keyID string,
	privateKey ed25519.PrivateKey,
) (SignedCleaningPlan, error) {
	return cleaningstep.BuildCleaningPlan(input, keyID, privateKey)
}
