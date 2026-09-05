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

package host

import (
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// Matches reports whether a Host satisfies a Machine's hostSelector. A nil selector
// matches every Available Host; callers are responsible for filtering by Eligibility
// separately, since selector matching and allocation eligibility are independent
// observations.
func Matches(hostLabels map[string]string, spec infrav1alpha1.TartHostSpec, selector *infrav1alpha1.HostSelector) bool {
	if selector == nil {
		return true
	}
	if selector.Architecture != "" && selector.Architecture != spec.Architecture {
		return false
	}
	if selector.FailureDomain != "" && selector.FailureDomain != spec.FailureDomain {
		return false
	}
	for k, v := range selector.MatchLabels {
		if hostLabels[k] != v {
			return false
		}
	}
	return true
}
