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
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

func DecideProvisionProgress(
	operation *infrastructurev1beta1.TartHostOperation,
) (model.ProvisionProgressDecisionResult, error) {
	route, err := DecideOperationRoute(OperationProvisioning{}, operation)
	if err != nil {
		return nil, fmt.Errorf("decide Provision TartHostOperation progress: %w", err)
	}
	switch route := route.(type) {
	case OperationProvisionFailedRoute:
		return model.ProvisionProgressFailed{
			Reason:  route.Reason,
			Message: ProvisionFailureMessage(route.Operation),
		}, nil
	case OperationProvisionHealthRoute:
		return model.ProvisionProgressAwaitingHealth{}, nil
	default:
		return nil, fmt.Errorf("unknown provisioning TartMachine route: %T", route)
	}
}

func ProvisionFailureMessage(operation *infrastructurev1beta1.TartHostOperation) string {
	return fmt.Sprintf("TartHostOperation %s/%s %s", operation.Namespace, operation.Name, operation.Status.Phase)
}

func PlanProvisionedStatus(
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) model.ProvisionedStatusResult {
	return model.ProvisionedStatusPlanned{
		Status: appprovisioning.StatusWithProvisioned(
			machine,
			machine.Status.Addresses,
			observation.ObservedMachineID,
			observation.ExpectedVersion,
		),
	}
}
