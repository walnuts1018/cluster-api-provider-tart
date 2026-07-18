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

package drivercapability

import (
	"context"
	"errors"
	"testing"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
)

func TestServiceObserveAndPersistPersistsDiscoveredCapabilities(t *testing.T) {
	t.Parallel()

	discoverer := &recordingDiscoverer{}
	writer := &recordingWriter{}
	service := NewService(discoverer, writer)
	target := testTarget(t)
	host := &infrastructurev1beta1.TartHost{}
	invocation := applicationdriver.Invocation{OperationType: "Provision", Phase: "PreparingBoot"}

	if err := service.ObserveAndPersist(t.Context(), driverdomain.Redfish, target, host, invocation); err != nil {
		t.Fatalf("ObserveAndPersist() error = %v", err)
	}
	if discoverer.calls != 1 {
		t.Fatalf("discoverer calls = %d, want 1", discoverer.calls)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1", writer.calls)
	}
	if got := writer.capabilities.Values(); len(got) != 2 {
		t.Fatalf("capabilities = %v, want 2 values", got)
	}
}

func TestServiceObserveAndPersistStopsOnDiscoveryError(t *testing.T) {
	t.Parallel()

	discoverer := &recordingDiscoverer{err: errors.New("boom")}
	writer := &recordingWriter{}
	service := NewService(discoverer, writer)

	err := service.ObserveAndPersist(
		t.Context(),
		driverdomain.Redfish,
		testTarget(t),
		&infrastructurev1beta1.TartHost{},
		applicationdriver.Invocation{},
	)
	if err == nil {
		t.Fatal("ObserveAndPersist() error = nil, want discovery failure")
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0", writer.calls)
	}
}

type recordingDiscoverer struct {
	calls int
	err   error
}

func (discoverer *recordingDiscoverer) DiscoverCapabilities(
	_ context.Context,
	_ driverdomain.Name,
	_ driverdomain.HostTarget,
	_ applicationdriver.Invocation,
) (capabilitydomain.Set, error) {
	discoverer.calls++
	if discoverer.err != nil {
		return capabilitydomain.Set{}, discoverer.err
	}
	return capabilitydomain.NewSet(capabilitydomain.PowerOn, capabilitydomain.PowerOff)
}

type recordingWriter struct {
	calls        int
	capabilities capabilitydomain.Set
}

func (writer *recordingWriter) UpdateCapabilities(
	_ context.Context,
	_ *infrastructurev1beta1.TartHost,
	capabilities capabilitydomain.Set,
) error {
	writer.calls++
	writer.capabilities = capabilities
	return nil
}

func testTarget(t *testing.T) driverdomain.HostTarget {
	t.Helper()
	address, err := driverdomain.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	return driverdomain.NewHostTarget(address)
}
