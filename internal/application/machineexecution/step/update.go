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
	"github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
)

func DecideUpdateOperation(
	operation *infrastructurev1beta1.TartHostOperation,
) (model.UpdateOperationDecisionResult, error) {
	route, err := DecideOperationRoute(OperationProvisioned{}, operation)
	if err != nil {
		return nil, fmt.Errorf("decide Update TartHostOperation outcome: %w", err)
	}
	switch route := route.(type) {
	case OperationUpdateTerminalRoute:
		return model.UpdateOperationApplyTerminal{Operation: route.Operation, Outcome: route.Outcome}, nil
	case OperationUpdateHealthRoute, OperationNodeHealthRoute:
		return model.UpdateOperationRouteNodeHealth{}, nil
	default:
		return nil, fmt.Errorf("unknown provisioned TartMachine route for Update Operation: %T", route)
	}
}
