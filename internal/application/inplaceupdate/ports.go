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

package inplaceupdate

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	nodelifecycleapp "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type PlanWriter interface {
	Write(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
		agentprotocol.ValidatedPlan,
		agentprotocol.Signature,
	) error
}

type NodeLifecyclePlanWriter interface {
	Write(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
		nodelifecycleapp.ValidatedPlan,
		agentprotocol.Signature,
	) error
}

type OperationStarter interface {
	Start(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
	) (*infrastructurev1beta1.TartHostOperation, error)
}
