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
	"crypto/ed25519"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	cleaningport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/port"
)

type Executor struct {
	hostPhase  cleaningport.HostPhaseService
	operations cleaningport.OperationService
	plans      cleaningport.PlanWriter
	signer     PlanSigner
}

func NewExecutor(
	hostPhase cleaningport.HostPhaseService,
	operations cleaningport.OperationService,
	plans cleaningport.PlanWriter,
	signer PlanSigner,
) *Executor {
	return &Executor{
		hostPhase:  hostPhase,
		operations: operations,
		plans:      plans,
		signer:     signer,
	}
}

type PlanSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
}

func (executor *Executor) MarkHostCleaning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	deletionPolicy infrastructurev1beta1.DeletionPolicy,
) error {
	return executor.hostPhase.MarkHostCleaningForDeletion(ctx, host, deletionPolicy)
}

func (executor *Executor) StartOperation(
	ctx context.Context,
	command StartOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return executor.operations.Start(ctx, command.Operation)
}

func (executor *Executor) PersistPlan(ctx context.Context, command PersistPlan) error {
	return executor.plans.Write(ctx, command.Operation, command.Plan, command.Signature)
}

func (executor *Executor) BuildSignedCleaningPlan(
	host *infrastructurev1beta1.TartHost,
	policy infrastructurev1beta1.DeletionPolicy,
	operation *infrastructurev1beta1.TartHostOperation,
) (SignedCleaningPlan, error) {
	return BuildSignedCleaningPlan(host, policy, operation, executor.signer)
}
