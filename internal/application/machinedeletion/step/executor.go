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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinedeletionport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion/port"
	machinedeletiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinedeletion"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type Observation struct {
	State machinedeletiondomain.Observation
	Host  *infrastructurev1beta1.TartHost
}

type Executor struct {
	client.Client
	Cleaner machinedeletionport.CleaningStep
}

func NewExecutor(k8sClient client.Client, cleaner machinedeletionport.CleaningStep) *Executor {
	return &Executor{Client: k8sClient, Cleaner: cleaner}
}

func (executor *Executor) Observe(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Observation, error) {
	if machine.Status.HostRef == nil {
		return Observation{State: machinedeletiondomain.ObservationHostReferenceAbsent{}}, nil
	}

	host, err := executor.referencedHost(ctx, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Observation{State: machinedeletiondomain.ObservationHostReferenceLost{}}, nil
		}
		return Observation{}, fmt.Errorf("get TartHost for delete reconcile: %w", err)
	}
	if host.UID != machine.Status.HostRef.UID {
		return Observation{State: machinedeletiondomain.ObservationHostReferenceLost{}}, nil
	}

	if machine.Status.OperationRef == nil {
		return Observation{State: machinedeletiondomain.ObservationHostReadyForCleaning{}, Host: host}, nil
	}

	operation, err := executor.referencedOperation(ctx, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Observation{State: machinedeletiondomain.ObservationCleaningOperationLost{}, Host: host}, nil
		}
		return Observation{}, fmt.Errorf("get Cleaning operation: %w", err)
	}
	if operation.UID != machine.Status.OperationRef.UID {
		return Observation{State: machinedeletiondomain.ObservationCleaningOperationLost{}, Host: host}, nil
	}

	if operation.Status.Phase == "" {
		return Observation{State: machinedeletiondomain.ObservationCleaningOperationRunning{}, Host: host}, nil
	}
	phase, err := operationdomain.ParsePhase(string(operation.Status.Phase))
	if err != nil {
		return Observation{}, fmt.Errorf("parse Cleaning operation phase: %w", err)
	}
	if !phase.Terminal() {
		return Observation{State: machinedeletiondomain.ObservationCleaningOperationRunning{Phase: phase}, Host: host}, nil
	}
	if phase == operationdomain.PhaseSucceeded {
		return Observation{State: machinedeletiondomain.ObservationCleaningOperationSucceeded{}, Host: host}, nil
	}
	return Observation{State: machinedeletiondomain.ObservationCleaningOperationFailed{Phase: phase}, Host: host}, nil
}

func (executor *Executor) StartCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) error {
	if executor.Cleaner == nil {
		return fmt.Errorf("start Cleaning operation: Cleaner is not configured")
	}
	operation, err := executor.Cleaner.StartCleaning(ctx, machine, host)
	if err != nil {
		return err
	}
	original := machine.DeepCopy()
	machine.Status.OperationRef = &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      operation.Name,
		UID:       operation.UID,
	}
	if err := executor.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("persist Cleaning operation reference: %w", err)
	}
	return nil
}

func (executor *Executor) ClearOperationReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	original := machine.DeepCopy()
	machine.Status.OperationRef = nil
	if err := executor.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("clear stale Cleaning operation reference: %w", err)
	}
	return nil
}

func (executor *Executor) referencedHost(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (*infrastructurev1beta1.TartHost, error) {
	host := &infrastructurev1beta1.TartHost{}
	if err := executor.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, host); err != nil {
		return nil, err
	}
	return host, nil
}

func (executor *Executor) referencedOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := executor.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}, operation); err != nil {
		return nil, err
	}
	return operation, nil
}
