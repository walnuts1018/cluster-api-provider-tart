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

package nodelifecycleengine

import (
	"context"
	"fmt"
	"maps"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/nodelifecycleengine"
)

type DriverSet struct {
	drivers map[domain.LifecycleRuntime]Driver
}

func NewDriverSet(drivers map[domain.LifecycleRuntime]Driver) *DriverSet {
	copied := make(map[domain.LifecycleRuntime]Driver, len(drivers))
	maps.Copy(copied, drivers)
	return &DriverSet{drivers: copied}
}

func (set *DriverSet) Preflight(ctx context.Context, plan domain.Plan) error {
	driver, err := set.driver(plan.LifecycleRuntime)
	if err != nil {
		return err
	}
	return driver.Preflight(ctx, plan)
}

func (set *DriverSet) CreateSnapshot(ctx context.Context, plan domain.Plan) (SnapshotResult, error) {
	driver, err := set.driver(plan.LifecycleRuntime)
	if err != nil {
		return SnapshotResult{}, err
	}
	return driver.CreateSnapshot(ctx, plan)
}

func (set *DriverSet) Apply(ctx context.Context, plan domain.Plan) error {
	driver, err := set.driver(plan.LifecycleRuntime)
	if err != nil {
		return err
	}
	return driver.Apply(ctx, plan)
}

func (set *DriverSet) ObserveHealth(ctx context.Context, plan domain.Plan) (domain.HealthInput, error) {
	driver, err := set.driver(plan.LifecycleRuntime)
	if err != nil {
		return domain.HealthInput{}, err
	}
	return driver.ObserveHealth(ctx, plan)
}

func (set *DriverSet) driver(runtime domain.LifecycleRuntime) (Driver, error) {
	if set == nil || set.drivers == nil {
		return nil, fmt.Errorf("node lifecycle engine set is required")
	}
	driver, ok := set.drivers[runtime]
	if !ok || driver == nil {
		return nil, fmt.Errorf("node lifecycle engine is not registered for %q", runtime)
	}
	return driver, nil
}
