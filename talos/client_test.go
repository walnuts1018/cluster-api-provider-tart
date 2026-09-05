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

package talos

import (
	"errors"
	"testing"
)

func TestClientVersionRejectsUnavailableClient(t *testing.T) {
	t.Parallel()

	var client *Client
	_, err := client.Version(t.Context())
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("Version() error = %v, want ErrClientUnavailable", err)
	}
}

func TestClientShutdownRejectsUnavailableClient(t *testing.T) {
	t.Parallel()

	var client *Client
	if err := client.Shutdown(t.Context()); !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("Shutdown() error = %v, want ErrClientUnavailable", err)
	}
}

func TestDialRejectsEmptyEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := DialMaintenance(t.Context(), " \t"); !errors.Is(err, ErrEndpointEmpty) {
		t.Fatalf("DialMaintenance() error = %v, want ErrEndpointEmpty", err)
	}
	if _, err := DialAuthenticated(t.Context(), "", nil, nil, nil); !errors.Is(err, ErrEndpointEmpty) {
		t.Fatalf("DialAuthenticated() error = %v, want ErrEndpointEmpty", err)
	}
}
