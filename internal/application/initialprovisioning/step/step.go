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
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
)

type Step interface {
	isInitialProvisioningStep()
}

type ReserveHost struct {
	Requirements allocationdomain.Requirements
}

type MarkHostReserved struct {
	Host *infrastructurev1beta1.TartHost
}

type StartOperation struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type CompleteOperation struct{}

type MarkHostProvisioned struct{}

func (ReserveHost) isInitialProvisioningStep()         {}
func (MarkHostReserved) isInitialProvisioningStep()    {}
func (StartOperation) isInitialProvisioningStep()      {}
func (CompleteOperation) isInitialProvisioningStep()   {}
func (MarkHostProvisioned) isInitialProvisioningStep() {}
