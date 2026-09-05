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

package wol

import (
	"context"
	"testing"

	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
	drivercontract "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver/contract"
)

func TestAdapterPowerOnContract(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	adapter := New(sender)
	if err := drivercontract.PowerOn(adapter); err != nil {
		t.Fatalf("PowerOn contract failed: %v", err)
	}

	if len(sender.addresses) != 2 {
		t.Fatalf("sent packets = %d, want 2", len(sender.addresses))
	}
	for _, address := range sender.addresses {
		if address != "00:00:5e:00:53:02" {
			t.Fatalf("sent address = %q, want normalized MAC", address)
		}
	}
}

func TestAdapterImplementsOnlyPowerOnPort(t *testing.T) {
	t.Parallel()

	adapter := any(New(&recordingSender{}))
	if _, ok := adapter.(applicationdriver.PowerOnDriver); !ok {
		t.Fatal("WoL adapter does not implement PowerOnDriver")
	}
	if _, ok := adapter.(applicationdriver.PowerOffDriver); ok {
		t.Fatal("WoL adapter unexpectedly implements PowerOffDriver")
	}
	if _, ok := adapter.(applicationdriver.PowerStateObserver); ok {
		t.Fatal("WoL adapter unexpectedly implements PowerStateObserver")
	}
	if _, ok := adapter.(applicationdriver.BootOverrideDriver); ok {
		t.Fatal("WoL adapter unexpectedly implements BootOverrideDriver")
	}
	if _, ok := adapter.(applicationdriver.VirtualMediaDriver); ok {
		t.Fatal("WoL adapter unexpectedly implements VirtualMediaDriver")
	}
}

type recordingSender struct {
	addresses []string
}

func (sender *recordingSender) Send(_ context.Context, address string) error {
	sender.addresses = append(sender.addresses, address)
	return nil
}
