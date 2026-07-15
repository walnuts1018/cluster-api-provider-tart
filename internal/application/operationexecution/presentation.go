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

package operationexecution

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func present(result operationdomain.Result) Result {
	switch selected := result.(type) {
	case operationdomain.ObserveActive, operationdomain.AwaitMachineHealth:
		return Result{RequeueAfter: DeadlineRequeueInterval}
	case operationdomain.Rejected:
		reason := failureReason(selected.Failure)
		message := failureMessage(selected.Failure)
		return Result{
			StatusCondition: &metav1.Condition{
				Type:    appupdate.ConditionDegraded,
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: message,
			},
			Event: &Event{
				Type:    "Warning",
				Reason:  reason,
				Message: message,
			},
		}
	default:
		return Result{}
	}
}

func failureReason(failure operationdomain.Failure) string {
	switch failure.(type) {
	case operationdomain.CleaningPolicyRequired:
		return "CleaningPolicyRequired"
	default:
		return "InvalidOperationState"
	}
}

func failureMessage(failure operationdomain.Failure) string {
	switch failure.(type) {
	case operationdomain.CleaningPolicyRequired:
		return "Clean operation requires a resolved deletion policy before it can continue"
	default:
		return "Operation state cannot be reconciled"
	}
}
