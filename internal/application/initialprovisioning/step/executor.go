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
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	initialprovisioningport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/port"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
)

type Executor struct {
	hostReserve initialprovisioningport.HostReserveService
	hostPhase   initialprovisioningport.HostPhaseService
	operations  initialprovisioningport.OperationService
}

func NewExecutor(
	hostReserve initialprovisioningport.HostReserveService,
	hostPhase initialprovisioningport.HostPhaseService,
	operations initialprovisioningport.OperationService,
) *Executor {
	return &Executor{
		hostReserve: hostReserve,
		hostPhase:   hostPhase,
		operations:  operations,
	}
}

func (executor *Executor) ReserveHost(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	command ReserveHost,
) (*infrastructurev1beta1.TartHost, error) {
	return executor.hostReserve.Reserve(ctx, machine, command.Requirements)
}

func (executor *Executor) RequirementsForMachine(
	machine *infrastructurev1beta1.TartMachine,
) (allocationdomain.Requirements, error) {
	return RequirementsForMachine(machine)
}

func (executor *Executor) BuildOperationDraft(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	planDigest string,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return BuildOperationDraft(machine, host, planDigest)
}

func (executor *Executor) MarkHostReserved(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) error {
	return executor.hostPhase.ReserveForMachine(ctx, host, machine)
}

func (executor *Executor) StartOperation(
	ctx context.Context,
	command StartOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return executor.operations.Start(ctx, command.Operation)
}

func (executor *Executor) CompleteProvisioning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if err := executor.operations.CompleteProvision(ctx, operation); err != nil {
		return fmt.Errorf("complete Provision operation: %w", err)
	}
	if err := executor.hostPhase.MarkHostProvisioned(ctx, host); err != nil {
		return fmt.Errorf("mark TartHost provisioned: %w", err)
	}
	return nil
}
