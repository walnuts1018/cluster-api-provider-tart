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

package distributionlifecycle

import (
	"context"
	"fmt"
	"maps"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

type DriverSet struct {
	drivers map[domain.Distribution]Driver
}

func NewDriverSet(drivers map[domain.Distribution]Driver) *DriverSet {
	copied := make(map[domain.Distribution]Driver, len(drivers))
	maps.Copy(copied, drivers)
	return &DriverSet{drivers: copied}
}

func (set *DriverSet) Preflight(ctx context.Context, plan domain.Plan) error {
	driver, err := set.driver(plan.Distribution)
	if err != nil {
		return err
	}
	return driver.Preflight(ctx, plan)
}

func (set *DriverSet) CreateSnapshot(ctx context.Context, plan domain.Plan) (SnapshotResult, error) {
	driver, err := set.driver(plan.Distribution)
	if err != nil {
		return SnapshotResult{}, err
	}
	return driver.CreateSnapshot(ctx, plan)
}

func (set *DriverSet) Apply(ctx context.Context, plan domain.Plan) error {
	driver, err := set.driver(plan.Distribution)
	if err != nil {
		return err
	}
	return driver.Apply(ctx, plan)
}

func (set *DriverSet) ObserveHealth(ctx context.Context, plan domain.Plan) (domain.HealthInput, error) {
	driver, err := set.driver(plan.Distribution)
	if err != nil {
		return domain.HealthInput{}, err
	}
	return driver.ObserveHealth(ctx, plan)
}

func (set *DriverSet) driver(distribution domain.Distribution) (Driver, error) {
	if set == nil || set.drivers == nil {
		return nil, fmt.Errorf("distribution lifecycle driver set is required")
	}
	driver, ok := set.drivers[distribution]
	if !ok || driver == nil {
		return nil, fmt.Errorf("distribution lifecycle driver is not registered for %q", distribution)
	}
	return driver, nil
}
