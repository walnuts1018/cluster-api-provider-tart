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

package platformprofile

import sharedprofile "github.com/walnuts1018/cluster-api-provider-tart/pkg/platformprofile"

// CredentialMode はPlatform Profileが最初のSession Credentialをどう扱うかを表す。
type CredentialMode string

const (
	CredentialModeMutualTLS         CredentialMode = "MutualTLS"
	CredentialModeSignedChallenge   CredentialMode = "SignedChallenge"
	CredentialModeBMCProtectedMedia CredentialMode = "BMCProtectedMedia"
	CredentialModeIsolatedL2        CredentialMode = "IsolatedL2"
)

// Requirement はPlatform Profileから導かれる初期認証の要件である。
type Requirement struct {
	Mode               CredentialMode
	IsolatedL2Required bool
}

// RequirementForProfile はPlatform Profile名から初期認証要件を返す。
// ここでは外部状態を読まないため、controllerから繰り返し呼んでも副作用を持たない。
func RequirementForProfile(profile string) (Requirement, bool) {
	if _, ok := sharedprofile.Lookup(profile); !ok {
		return Requirement{}, false
	}
	return Requirement{
		Mode:               CredentialModeIsolatedL2,
		IsolatedL2Required: true,
	}, true
}
