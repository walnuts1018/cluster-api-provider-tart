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

package hostallocation

import (
	"strings"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

func TestPresentFailureMapsUnsupportedCapability(t *testing.T) {
	t.Parallel()

	presentation := PresentFailure(domain.UnsupportedCapability{
		Missing: []capability.Capability{capability.PowerOn, capability.SetNextBoot},
	})

	if presentation.Reason != "UnsupportedCapability" {
		t.Fatalf("Reason = %q, want UnsupportedCapability", presentation.Reason)
	}
	if !strings.Contains(presentation.Message, "PowerOn") || !strings.Contains(presentation.Message, "SetNextBoot") {
		t.Fatalf("Message = %q, want missing capability names", presentation.Message)
	}
}
