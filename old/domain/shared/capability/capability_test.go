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

package capability

import (
	"errors"
	"slices"
	"testing"
)

func TestCapabilityParse(t *testing.T) {
	t.Parallel()

	for _, capability := range All() {
		t.Run(string(capability), func(t *testing.T) {
			t.Parallel()

			got, err := Parse(string(capability))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got != capability {
				t.Fatalf("Parse() = %q, want %q", got, capability)
			}
		})
	}

	if _, err := Parse("NetworkBoot"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Parse() error = %v, want %v", err, ErrUnknown)
	}
}

func TestSet(t *testing.T) {
	t.Parallel()

	available, err := NewSet(PowerOn, SetNextBoot, PowerOn)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	required, err := NewSet(PowerOn, SetNextBoot)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	unsupported, err := NewSet(PowerOn, PowerOff)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	if !available.ContainsAll(required) {
		t.Fatal("ContainsAll() = false, want true")
	}
	if available.ContainsAll(unsupported) {
		t.Fatal("ContainsAll() = true, want false")
	}
	if got := available.Values(); !slices.Equal(got, []Capability{PowerOn, SetNextBoot}) {
		t.Fatalf("Values() = %v, want [PowerOn SetNextBoot]", got)
	}
}

func TestNewSetRejectsUnknownCapability(t *testing.T) {
	t.Parallel()

	if _, err := NewSet(Capability("unknown")); !errors.Is(err, ErrUnknown) {
		t.Fatalf("NewSet() error = %v, want %v", err, ErrUnknown)
	}
}
