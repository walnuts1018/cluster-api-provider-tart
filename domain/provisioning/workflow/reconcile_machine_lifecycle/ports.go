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

package machinelifecycle

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/delete_machine"
	machineexecution "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/reconcile_machine"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/resourcefinalizer"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type FinalizerPort interface {
	Ensure(context.Context, client.Object) (resourcefinalizer.Result, error)
	Release(context.Context, client.Object) (resourcefinalizer.Result, error)
	Present(client.Object) bool
}

type ExecutionWorkflow interface {
	Do(context.Context, machineexecution.Command) sharedresult.Result[machineexecution.Event, sharedworkflow.Failure]
}

type DeletionWorkflow interface {
	Do(context.Context, machinedeletion.Command) sharedresult.Result[machinedeletion.Event, sharedworkflow.Failure]
}

type Ports struct {
	Finalizer FinalizerPort
	Execution ExecutionWorkflow
	Deletion  DeletionWorkflow
}
