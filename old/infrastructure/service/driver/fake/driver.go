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

package fake

import (
	"context"
	"sync"

	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

type PowerOnCall struct {
	Target      driverdomain.HostTarget
	OperationID operationdomain.ID
}

type Driver struct {
	mu            sync.Mutex
	powerOnErrors []error
	powerOnCalls  []PowerOnCall
}

func NewPowerOn(errors ...error) *Driver {
	return &Driver{powerOnErrors: append([]error(nil), errors...)}
}

func (driver *Driver) PowerOn(
	ctx context.Context,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	driver.powerOnCalls = append(driver.powerOnCalls, PowerOnCall{
		Target:      target,
		OperationID: operationID,
	})
	index := len(driver.powerOnCalls) - 1
	if index < len(driver.powerOnErrors) {
		return driver.powerOnErrors[index]
	}
	return nil
}

func (driver *Driver) PowerOnCalls() []PowerOnCall {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return append([]PowerOnCall(nil), driver.powerOnCalls...)
}
