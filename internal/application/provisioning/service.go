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

package provisioning

import (
	"context"
	"fmt"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type PowerOnService interface {
	PowerOn(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		operationdomain.ID,
		applicationdriver.Invocation,
	) error
}

type HostReader interface {
	GetAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error)
}

type HostProvisioner interface {
	MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
}

type Service interface {
	Begin(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
	Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
}

type service struct {
	hostReader      HostReader
	hostProvisioner HostProvisioner
	power           PowerOnService
}

func NewService(hostReader HostReader, hostProvisioner HostProvisioner, power PowerOnService) Service {
	return &service{
		hostReader:      hostReader,
		hostProvisioner: hostProvisioner,
		power:           power,
	}
}

func (s *service) Begin(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
	bootMACAddress, err := driverdomain.ParseMACAddress(hostdomain.BootMACAddress(host))
	if err != nil {
		return fmt.Errorf("parse TartHost boot MAC address: %w", err)
	}
	operationID, err := operationdomain.DeterministicID(string(host.UID) + "/" + machineReferenceUID(host))
	if err != nil {
		return fmt.Errorf("derive legacy provisioning operation ID: %w", err)
	}
	if err := s.power.PowerOn(
		ctx,
		driverdomain.WoL,
		driverdomain.NewHostTarget(bootMACAddress),
		operationID,
		applicationdriver.Invocation{
			OperationType: "Provision",
			Phase:         "PreparingBoot",
			Rollback:      false,
		},
	); err != nil {
		return fmt.Errorf("power on TartHost: %w", err)
	}
	return s.hostProvisioner.MarkProvisioning(ctx, host)
}

func machineReferenceUID(host *infrastructurev1alpha1.TartHost) string {
	if host.Status.MachineRef == nil {
		return "unassigned"
	}
	return string(host.Status.MachineRef.UID)
}

func (s *service) Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	if machine.Status.HostRef == nil {
		return nil
	}

	host, err := s.hostReader.GetAssigned(ctx, machine)
	if err != nil {
		return err
	}

	if !hostdomain.MachineRefMatches(host.Status.MachineRef, machine) {
		return nil
	}

	if host.Status.State == infrastructurev1alpha1.TartHostStateProvisioning ||
		host.Status.State == infrastructurev1alpha1.TartHostStateProvisioned {
		return nil
	}

	return s.Begin(ctx, host)
}
