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
	"errors"
	"slices"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

var ErrNoEligibleHost = errors.New("no eligible host")

// SelectFreshは新規allocation用の候補をname順で一つ選ぶ。RetainedやReusableを
// 通常のMachine allocationへ混ぜないことで、data保持中のHostを暗黙に再利用しない。
func SelectFresh(hosts []infrav1alpha1.TartHost, selector *infrav1alpha1.HostSelector) (*infrav1alpha1.TartHost, error) {
	candidates := make([]infrav1alpha1.TartHost, 0, len(hosts))
	for _, candidate := range hosts {
		if Classify(candidate.Spec) != Available || candidate.Spec.ID == "" {
			continue
		}
		if !Matches(candidate.Labels, candidate.Spec, selector) {
			continue
		}
		candidates = append(candidates, *candidate.DeepCopy())
	}
	if len(candidates) == 0 {
		return nil, ErrNoEligibleHost
	}
	slices.SortFunc(candidates, func(left, right infrav1alpha1.TartHost) int {
		if left.Name < right.Name {
			return -1
		}
		if left.Name > right.Name {
			return 1
		}
		return 0
	})
	return &candidates[0], nil
}
