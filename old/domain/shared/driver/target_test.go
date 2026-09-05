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

package driver

import (
	"errors"
	"testing"
)

func TestParseMACAddress(t *testing.T) {
	t.Parallel()

	address, err := ParseMACAddress("00-00-5E-00-53-02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	if got := address.String(); got != "00:00:5e:00:53:02" {
		t.Fatalf("MACAddress.String() = %q, want %q", got, "00:00:5e:00:53:02")
	}
}

func TestParseMACAddressRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseMACAddress("invalid"); !errors.Is(err, ErrInvalidMACAddress) {
		t.Fatalf("ParseMACAddress() error = %v, want ErrInvalidMACAddress", err)
	}
}

func TestHostTargetWithRedfishAccess(t *testing.T) {
	t.Parallel()

	address, err := ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	access, err := NewRedfishAccess(
		"https://bmc.example.test",
		"admin",
		"secret",
		nil,
		[]string{"sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	)
	if err != nil {
		t.Fatalf("NewRedfishAccess() error = %v", err)
	}

	target := NewHostTarget(address).WithRedfishAccess(access)
	got, ok := target.RedfishAccess()
	if !ok {
		t.Fatal("RedfishAccess() ok = false, want true")
	}
	if got.Endpoint() != "https://bmc.example.test" {
		t.Fatalf("Endpoint() = %q, want %q", got.Endpoint(), "https://bmc.example.test")
	}
	if got.Username() != "admin" || got.Password() != "secret" {
		t.Fatalf("credentials = %q/%q, want admin/secret", got.Username(), got.Password())
	}
}

func TestNewRedfishAccessRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		endpoint string
		pins     []string
		wantErr  error
	}{
		{name: "empty endpoint", wantErr: ErrInvalidEndpoint},
		{name: "non-https", endpoint: "http://bmc.example.test", wantErr: ErrInvalidEndpoint},
		{name: "invalid pin", endpoint: "https://bmc.example.test", pins: []string{"invalid"}, wantErr: ErrInvalidSPKIPin},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewRedfishAccess(tt.endpoint, "user", "pass", nil, tt.pins); !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewRedfishAccess() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
