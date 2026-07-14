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

package model

import (
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

type ProvisionReferenceResult interface {
	isProvisionReferenceResult()
}

type ProvisionReferenceReady struct{}

type ProvisionReferenceBlocked struct{}

func (ProvisionReferenceReady) isProvisionReferenceResult()   {}
func (ProvisionReferenceBlocked) isProvisionReferenceResult() {}

type BootstrapReadinessResult interface {
	isBootstrapReadinessResult()
}

type BootstrapDataReady struct{}

type BootstrapDataWaiting struct{}

func (BootstrapDataReady) isBootstrapReadinessResult()   {}
func (BootstrapDataWaiting) isBootstrapReadinessResult() {}

type ProvisionHostReservationResult interface {
	isProvisionHostReservationResult()
}

type ProvisionHostReservationNoHost struct{}

type ProvisionHostReservationStarted struct {
	Started appprovisioning.StartResult
}

func (ProvisionHostReservationNoHost) isProvisionHostReservationResult()  {}
func (ProvisionHostReservationStarted) isProvisionHostReservationResult() {}

type ProviderIDStepResult interface {
	isProviderIDStepResult()
}

type ProviderIDAlreadySet struct{}

type ProviderIDPatched struct{}

func (ProviderIDAlreadySet) isProviderIDStepResult() {}
func (ProviderIDPatched) isProviderIDStepResult()    {}

type ProvisionStartStatusPatch interface {
	isProvisionStartStatusPatch()
}

type ProvisionStartStatusWaitingForBootstrap struct{}

type ProvisionStartStatusNoAvailableHost struct{}

type ProvisionStartStatusHostReserved struct {
	Host      *infrastructurev1beta1.TartHost
	Operation *infrastructurev1beta1.TartHostOperation
}

func (ProvisionStartStatusWaitingForBootstrap) isProvisionStartStatusPatch() {}
func (ProvisionStartStatusNoAvailableHost) isProvisionStartStatusPatch()     {}
func (ProvisionStartStatusHostReserved) isProvisionStartStatusPatch()        {}

type ProvisionStartStatusPatchResult interface {
	isProvisionStartStatusPatchResult()
}

type ProvisionStartStatusPatched struct{}

func (ProvisionStartStatusPatched) isProvisionStartStatusPatchResult() {}

type ProvisionCompletionHostResult interface {
	isProvisionCompletionHostResult()
}

type ProvisionCompletionHostResolved struct {
	Host *infrastructurev1beta1.TartHost
}

func (ProvisionCompletionHostResolved) isProvisionCompletionHostResult() {}

type ProvisionCompletionEffectResult interface {
	isProvisionCompletionEffectResult()
}

type ProvisionCompletionEffectApplied struct{}

func (ProvisionCompletionEffectApplied) isProvisionCompletionEffectResult() {}

type ProvisionedStatusResult interface {
	isProvisionedStatusResult()
}

type ProvisionedStatusPlanned struct {
	Status infrastructurev1beta1.TartMachineStatus
}

func (ProvisionedStatusPlanned) isProvisionedStatusResult() {}

type ProvisionHealthGateDecisionResult interface {
	isProvisionHealthGateDecisionResult()
}

type ProvisionHealthGateComplete struct {
	Operation   *infrastructurev1beta1.TartHostOperation
	Observation machinehealthdomain.NodeObservation
}

type ProvisionHealthGatePending struct {
	Reason  string
	Message string
}

func (ProvisionHealthGateComplete) isProvisionHealthGateDecisionResult() {}
func (ProvisionHealthGatePending) isProvisionHealthGateDecisionResult()  {}

type ProvisionProgressReferenceResult interface {
	isProvisionProgressReferenceResult()
}

type ProvisionProgressReferenceAbsent struct{}

type ProvisionProgressReferenceStale struct {
	Reference *infrastructurev1beta1.ResourceReference
}

type ProvisionProgressReferenceResolved struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

func (ProvisionProgressReferenceAbsent) isProvisionProgressReferenceResult()   {}
func (ProvisionProgressReferenceStale) isProvisionProgressReferenceResult()    {}
func (ProvisionProgressReferenceResolved) isProvisionProgressReferenceResult() {}

type StaleProvisionOperationReferenceCleared struct {
	Reference *infrastructurev1beta1.ResourceReference
}

type ProvisionProgressDecisionResult interface {
	isProvisionProgressDecisionResult()
}

type ProvisionProgressAwaitingHealth struct{}

type ProvisionProgressFailed struct {
	Reason  string
	Message string
}

func (ProvisionProgressAwaitingHealth) isProvisionProgressDecisionResult() {}
func (ProvisionProgressFailed) isProvisionProgressDecisionResult()         {}

type ProvisionFailureStatusPatchResult struct {
	Reason  string
	Message string
}
