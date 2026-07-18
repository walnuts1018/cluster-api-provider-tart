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

package clusterlifecycle

import (
	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster_status"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/resourcefinalizer"
)

type Result interface {
	isResult()
}

type ResultActiveReconciled struct {
	Finalizer resourcefinalizer.Result
	Status    clusterstatus.Result
}

type ResultFinalizerReleased struct {
	Finalizer resourcefinalizer.Result
}

func (ResultActiveReconciled) isResult()  {}
func (ResultFinalizerReleased) isResult() {}
