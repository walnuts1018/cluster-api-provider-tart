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

package machinedeletion

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinedeletiondomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinedeletion"
	cleaning "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/do_cleaning"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

type Observation struct {
	State machinedeletiondomain.Observation
	Host  *infrastructurev1beta1.TartHost
}

type runtime struct {
	client.Client
	Cleaner CleaningWorkflow
}

func newRuntime(k8sClient client.Client, cleaner CleaningWorkflow) *runtime {
	return &runtime{Client: k8sClient, Cleaner: cleaner}
}

func (runtime *runtime) observe(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Observation, error) {
	if machine.Status.HostRef == nil {
		return Observation{State: machinedeletiondomain.ObservationHostReferenceAbsent{}}, nil
	}

	host, err := runtime.referencedHost(ctx, machine)
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

	operation, err := runtime.referencedOperation(ctx, machine)
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

func (runtime *runtime) startCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) error {
	if runtime.Cleaner == nil {
		return fmt.Errorf("start Cleaning operation: Cleaner is not configured")
	}
	outcome := runtime.Cleaner.Do(ctx, cleaning.Command{Machine: machine, Host: host})
	event, present := outcome.Value().Value()
	if !present {
		failure, _ := outcome.FailureValue().Value()
		return fmt.Errorf("start cleaning: %s", failure.Message())
	}
	operation := event.Operation
	original := machine.DeepCopy()
	machine.Status.OperationRef = &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      operation.Name,
		UID:       operation.UID,
	}
	if err := runtime.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("persist Cleaning operation reference: %w", err)
	}
	return nil
}

func (runtime *runtime) clearOperationReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	original := machine.DeepCopy()
	machine.Status.OperationRef = nil
	if err := runtime.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("clear stale Cleaning operation reference: %w", err)
	}
	return nil
}

func (runtime *runtime) referencedHost(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (*infrastructurev1beta1.TartHost, error) {
	host := &infrastructurev1beta1.TartHost{}
	if err := runtime.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, host); err != nil {
		return nil, err
	}
	return host, nil
}

func (runtime *runtime) referencedOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := runtime.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}, operation); err != nil {
		return nil, err
	}
	return operation, nil
}
