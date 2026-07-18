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

package machineexecution

import (
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
)

type nodeHealthPorts interface {
	ObserveNodeHealth(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machineexecutionmodel.NodeHealthResult, error)
}

type nodeHealth struct {
	steps      nodeHealthPorts
	healthGate *healthGate
}

func newNodeHealth(steps nodeHealthPorts, healthGate *healthGate) *nodeHealth {
	return &nodeHealth{
		steps:      steps,
		healthGate: healthGate,
	}
}

func (handler *nodeHealth) run(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	nodeHealth, err := handler.steps.ObserveNodeHealth(ctx, machine)
	if err != nil {
		return err
	}
	switch nodeHealth := nodeHealth.(type) {
	case machineexecutionmodel.NodeHealthUnavailable:
		return nil
	case machineexecutionmodel.NodeHealthObserved:
		return handler.healthGate.run(ctx, machine, nodeHealth.Observation)
	default:
		return fmt.Errorf("unknown Node health result: %T", nodeHealth)
	}
}
