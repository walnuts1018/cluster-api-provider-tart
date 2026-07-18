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

package slot

import "testing"

func TestInactive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		active Slot
		want   Slot
	}{
		{name: "Aの反対はB", active: A, want: B},
		{name: "Bの反対はA", active: B, want: A},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, present := tt.active.Inactive().Value().Value()
			if !present {
				t.Fatal("Inactive() returned failure")
			}
			if got != tt.want {
				t.Fatalf("Inactive() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlotRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	parseFailure, present := Parse("C").FailureValue().Value()
	if !present {
		t.Fatal("Parse() returned success")
	}
	if unknown, ok := parseFailure.(Unknown); !ok || unknown.Value != "C" {
		t.Fatalf("Parse() failure = %#v, want Unknown{Value: %q}", parseFailure, "C")
	}
	inactiveFailure, present := Slot("C").Inactive().FailureValue().Value()
	if !present {
		t.Fatal("Inactive() returned success")
	}
	if unknown, ok := inactiveFailure.(Unknown); !ok || unknown.Value != "C" {
		t.Fatalf("Inactive() failure = %#v, want Unknown{Value: %q}", inactiveFailure, "C")
	}
}
