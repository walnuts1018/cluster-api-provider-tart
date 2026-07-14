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
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
)

type provisionReferenceResult interface {
	isProvisionReferenceResult()
}

type provisionReferenceReady struct{}

type provisionReferenceBlocked struct{}

func (provisionReferenceReady) isProvisionReferenceResult()   {}
func (provisionReferenceBlocked) isProvisionReferenceResult() {}

type provisionStartDependencyResult interface {
	isProvisionStartDependencyResult()
}

type provisionStartDependencyUnavailable struct{}

type provisionStartDependencyAvailable struct {
	Provisioner ProvisionWorkflow
}

func (provisionStartDependencyUnavailable) isProvisionStartDependencyResult() {}
func (provisionStartDependencyAvailable) isProvisionStartDependencyResult()   {}

type bootstrapReadinessResult interface {
	isBootstrapReadinessResult()
}

type bootstrapDataReady struct{}

type bootstrapDataWaiting struct{}

func (bootstrapDataReady) isBootstrapReadinessResult()   {}
func (bootstrapDataWaiting) isBootstrapReadinessResult() {}

type provisionHostReservationResult interface {
	isProvisionHostReservationResult()
}

type provisionHostReservationNoHost struct{}

type provisionHostReservationStarted struct {
	Started appprovisioning.StartResult
}

func (provisionHostReservationNoHost) isProvisionHostReservationResult()  {}
func (provisionHostReservationStarted) isProvisionHostReservationResult() {}

type providerIDStepResult interface {
	isProviderIDStepResult()
}

type providerIDAlreadySet struct{}

type providerIDPatched struct{}

func (providerIDAlreadySet) isProviderIDStepResult() {}
func (providerIDPatched) isProviderIDStepResult()    {}

type provisionStartStatusPatch interface {
	isProvisionStartStatusPatch()
}

type provisionStartStatusWaitingForBootstrap struct{}

type provisionStartStatusNoAvailableHost struct{}

type provisionStartStatusHostReserved struct {
	Host      *infrastructurev1beta1.TartHost
	Operation *infrastructurev1beta1.TartHostOperation
}

func (provisionStartStatusWaitingForBootstrap) isProvisionStartStatusPatch() {}
func (provisionStartStatusNoAvailableHost) isProvisionStartStatusPatch()     {}
func (provisionStartStatusHostReserved) isProvisionStartStatusPatch()        {}

type provisionStartStatusPatchResult interface {
	isProvisionStartStatusPatchResult()
}

type provisionStartStatusPatched struct{}

func (provisionStartStatusPatched) isProvisionStartStatusPatchResult() {}

type provisionCompletionDependencyResult interface {
	isProvisionCompletionDependencyResult()
}

type provisionCompletionDependencyAvailable struct {
	Provisioner ProvisionWorkflow
}

type provisionCompletionDependencyMissing struct{}

func (provisionCompletionDependencyAvailable) isProvisionCompletionDependencyResult() {}
func (provisionCompletionDependencyMissing) isProvisionCompletionDependencyResult()   {}

type provisionCompletionHostResult interface {
	isProvisionCompletionHostResult()
}

type provisionCompletionHostResolved struct {
	Host *infrastructurev1beta1.TartHost
}

func (provisionCompletionHostResolved) isProvisionCompletionHostResult() {}

type provisionCompletionEffectResult interface {
	isProvisionCompletionEffectResult()
}

type provisionCompletionEffectApplied struct{}

func (provisionCompletionEffectApplied) isProvisionCompletionEffectResult() {}

type provisionedStatusResult interface {
	isProvisionedStatusResult()
}

type provisionedStatusPlanned struct {
	Status infrastructurev1beta1.TartMachineStatus
}

func (provisionedStatusPlanned) isProvisionedStatusResult() {}

type provisionProgressReferenceResult interface {
	isProvisionProgressReferenceResult()
}

type provisionProgressReferenceAbsent struct{}

type provisionProgressReferenceStale struct {
	Reference *infrastructurev1beta1.ResourceReference
}

type provisionProgressReferenceResolved struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

func (provisionProgressReferenceAbsent) isProvisionProgressReferenceResult()   {}
func (provisionProgressReferenceStale) isProvisionProgressReferenceResult()    {}
func (provisionProgressReferenceResolved) isProvisionProgressReferenceResult() {}

type staleProvisionOperationReferenceCleared struct {
	Reference *infrastructurev1beta1.ResourceReference
}

type provisionProgressDecisionResult interface {
	isProvisionProgressDecisionResult()
}

type provisionProgressAwaitingHealth struct{}

type provisionProgressFailed struct {
	Reason  string
	Message string
}

func (provisionProgressAwaitingHealth) isProvisionProgressDecisionResult() {}
func (provisionProgressFailed) isProvisionProgressDecisionResult()         {}

type provisionFailureStatusPatchResult struct {
	Reason  string
	Message string
}
