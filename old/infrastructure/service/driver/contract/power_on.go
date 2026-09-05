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

package contract

import (
	"context"
	"errors"
	"fmt"

	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
)

func PowerOn(implementation applicationdriver.PowerOnDriver) error {
	address, err := driverdomain.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		return fmt.Errorf("parse contract MAC address: %w", err)
	}
	operationID, err := operationdomain.ParseID("f4353748-c9ea-41c6-b321-94197b64330e")
	if err != nil {
		return fmt.Errorf("parse contract operation ID: %w", err)
	}
	target := driverdomain.NewHostTarget(address)

	if err := implementation.PowerOn(context.Background(), target, operationID); err != nil {
		return fmt.Errorf("first PowerOn: %w", err)
	}
	if err := implementation.PowerOn(context.Background(), target, operationID); err != nil {
		return fmt.Errorf("repeat PowerOn with the same operation ID: %w", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := implementation.PowerOn(cancelled, target, operationID); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("PowerOn with cancelled context error = %w, want context.Canceled", err)
	}
	return nil
}
