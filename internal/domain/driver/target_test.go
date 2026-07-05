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
