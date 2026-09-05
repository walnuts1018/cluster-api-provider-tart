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

package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// setNotImplemented reports a resource as safely stopped because the reconcile logic
// for its Condition type has not been implemented yet. It never performs an external
// side effect, so it is always safe to report while the real policy is still being
// built out in later sessions. See docs/development/decisions.md.
func setNotImplemented(conditions *[]metav1.Condition, conditionType string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionFalse,
		Reason:             infrav1alpha1.ReasonNotImplemented,
		Message:            "Reconcile logic for this resource is not implemented yet; no external side effect has been attempted.",
		ObservedGeneration: generation,
	})
}
