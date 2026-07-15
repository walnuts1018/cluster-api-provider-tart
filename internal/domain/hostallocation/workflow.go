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
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

func Decide(command Command) Result {
	var unsupported []capability.Capability

	for _, candidate := range command.Candidates {
		accepted, missing := evaluate(command.Machine, command.Requirements, candidate)
		if accepted {
			return Allocated{
				Host: candidate.Host,
				Events: []Event{
					HostSelected{
						Host:    candidate.Host,
						Machine: command.Machine,
					},
				},
			}
		}
		if len(missing) > 0 && len(unsupported) == 0 {
			unsupported = missing
		}
	}

	if len(unsupported) > 0 {
		return NotAllocated{Failure: UnsupportedCapability{Missing: unsupported}}
	}
	return NotAllocated{Failure: NoMatchingHost{Requirements: command.Requirements}}
}

func evaluate(machine MachineRef, requirements Requirements, candidate Candidate) (bool, []capability.Capability) {
	switch assignment := candidate.Assignment.(type) {
	case Unassigned:
		if candidate.Phase != hostdomain.PhaseAvailable {
			return false, nil
		}
	case AssignedToMachine:
		if assignment.Machine != machine {
			return false, nil
		}
	default:
		return false, nil
	}

	if candidate.Architecture != requirements.Architecture {
		return false, nil
	}
	if candidate.Firmware != requirements.Firmware {
		return false, nil
	}
	if candidate.PlatformProfile != requirements.PlatformProfile {
		return false, nil
	}
	if candidate.RootDiskSizeBytes < requirements.MinRootDiskBytes {
		return false, nil
	}
	if !containsLabels(candidate.Labels, requirements.MatchLabels) {
		return false, nil
	}

	missing := missingCapabilities(candidate.Capabilities, requirements.Capabilities)
	if len(missing) > 0 {
		return false, missing
	}
	return true, nil
}

func containsLabels(actual, required map[string]string) bool {
	for key, value := range required {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func missingCapabilities(actual, required capability.Set) []capability.Capability {
	missing := make([]capability.Capability, 0)
	for _, value := range required.Values() {
		if !actual.Has(value) {
			missing = append(missing, value)
		}
	}
	return missing
}
