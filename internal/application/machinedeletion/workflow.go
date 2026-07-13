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
	machinedeletiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinedeletion"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type CleaningWorkflow interface {
	StartCleaning(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		host *infrastructurev1beta1.TartHost,
	) (*infrastructurev1beta1.TartHostOperation, error)
}

type Result interface {
	isResult()
}

type ResultWaiting struct{}
type ResultFinalizerReady struct{}

func (ResultWaiting) isResult()        {}
func (ResultFinalizerReady) isResult() {}

type Workflow struct {
	client.Client
	Cleaner CleaningWorkflow
}

func NewWorkflow(k8sClient client.Client, cleaner CleaningWorkflow) *Workflow {
	return &Workflow{Client: k8sClient, Cleaner: cleaner}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	observation, host, err := workflow.observe(ctx, machine)
	if err != nil {
		return nil, err
	}

	command, err := machinedeletiondomain.Decide(observation)
	if err != nil {
		return nil, err
	}
	switch command := command.(type) {
	case machinedeletiondomain.CommandReleaseFinalizer:
		return ResultFinalizerReady{}, nil
	case machinedeletiondomain.CommandStartCleaning:
		if host == nil {
			return nil, fmt.Errorf("TartHost is required to start Cleaning operation")
		}
		return ResultWaiting{}, workflow.startCleaning(ctx, machine, host)
	case machinedeletiondomain.CommandClearOperationReference:
		return ResultWaiting{}, workflow.clearOperationReference(ctx, machine)
	case machinedeletiondomain.CommandWaitCleaning:
		return ResultWaiting{}, nil
	case machinedeletiondomain.CommandFailCleaning:
		return nil, fmt.Errorf("Cleaning operation finished in %s", command.Phase)
	default:
		return nil, fmt.Errorf("unknown TartMachine deletion command: %T", command)
	}
}

func (workflow *Workflow) observe(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (machinedeletiondomain.Observation, *infrastructurev1beta1.TartHost, error) {
	if machine.Status.HostRef == nil {
		return machinedeletiondomain.ObservationHostReferenceAbsent{}, nil, nil
	}

	host, err := workflow.referencedHost(ctx, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return machinedeletiondomain.ObservationHostReferenceLost{}, nil, nil
		}
		return nil, nil, fmt.Errorf("get TartHost for delete reconcile: %w", err)
	}
	if host.UID != machine.Status.HostRef.UID {
		return machinedeletiondomain.ObservationHostReferenceLost{}, nil, nil
	}

	if machine.Status.OperationRef == nil {
		return machinedeletiondomain.ObservationHostReadyForCleaning{}, host, nil
	}

	operation, err := workflow.referencedOperation(ctx, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return machinedeletiondomain.ObservationCleaningOperationLost{}, host, nil
		}
		return nil, nil, fmt.Errorf("get Cleaning operation: %w", err)
	}
	if operation.UID != machine.Status.OperationRef.UID {
		return machinedeletiondomain.ObservationCleaningOperationLost{}, host, nil
	}

	if operation.Status.Phase == "" {
		return machinedeletiondomain.ObservationCleaningOperationRunning{}, host, nil
	}
	phase, err := operationdomain.ParsePhase(string(operation.Status.Phase))
	if err != nil {
		return nil, nil, fmt.Errorf("parse Cleaning operation phase: %w", err)
	}
	if !phase.Terminal() {
		return machinedeletiondomain.ObservationCleaningOperationRunning{Phase: phase}, host, nil
	}
	if phase == operationdomain.PhaseSucceeded {
		return machinedeletiondomain.ObservationCleaningOperationSucceeded{}, host, nil
	}
	return machinedeletiondomain.ObservationCleaningOperationFailed{Phase: phase}, host, nil
}

func (workflow *Workflow) referencedHost(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (*infrastructurev1beta1.TartHost, error) {
	host := &infrastructurev1beta1.TartHost{}
	if err := workflow.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, host); err != nil {
		return nil, err
	}
	return host, nil
}

func (workflow *Workflow) referencedOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := workflow.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}, operation); err != nil {
		return nil, err
	}
	return operation, nil
}

func (workflow *Workflow) startCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) error {
	if workflow.Cleaner == nil {
		return fmt.Errorf("start Cleaning operation: Cleaner is not configured")
	}
	operation, err := workflow.Cleaner.StartCleaning(ctx, machine, host)
	if err != nil {
		return err
	}
	original := machine.DeepCopy()
	machine.Status.OperationRef = &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      operation.Name,
		UID:       operation.UID,
	}
	if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("persist Cleaning operation reference: %w", err)
	}
	return nil
}

func (workflow *Workflow) clearOperationReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	original := machine.DeepCopy()
	machine.Status.OperationRef = nil
	if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("clear stale Cleaning operation reference: %w", err)
	}
	return nil
}
