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
	"fmt"
	"strings"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

type FailurePresentation struct {
	Reason       string
	Message      string
	RequeueAfter time.Duration
}

func PresentFailure(failure domain.Failure) FailurePresentation {
	switch failure := failure.(type) {
	case domain.NoMatchingHost:
		return FailurePresentation{
			Reason:       "NoAvailableHost",
			Message:      "No available TartHost matches the requirements; will retry",
			RequeueAfter: 30 * time.Second,
		}
	case domain.UnsupportedCapability:
		return FailurePresentation{
			Reason:       "UnsupportedCapability",
			Message:      fmt.Sprintf("No TartHost satisfies the required capabilities: %s", joinCapabilities(failure.Missing)),
			RequeueAfter: 30 * time.Second,
		}
	default:
		panic(fmt.Sprintf("unknown host allocation failure: %T", failure))
	}
}

func joinCapabilities(values []capability.Capability) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, ", ")
}
