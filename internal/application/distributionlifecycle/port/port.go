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

package port

import (
	"context"

	distributionlifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle/model"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

type DistributionLifecycleDriver interface {
	Preflight(context.Context, domain.Plan) error
	CreateSnapshot(context.Context, domain.Plan) (distributionlifecyclemodel.SnapshotResult, error)
	Apply(context.Context, domain.Plan) error
	Verify(context.Context, domain.Plan) error
}
