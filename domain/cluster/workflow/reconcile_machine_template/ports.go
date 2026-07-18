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

package machinetemplatelifecycle

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/resourcefinalizer"
)

type FinalizerPort interface {
	Ensure(context.Context, client.Object) (resourcefinalizer.Result, error)
	Release(context.Context, client.Object) (resourcefinalizer.Result, error)
}

type Ports struct {
	Finalizer FinalizerPort
}
