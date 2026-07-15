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
	"slices"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

func TestDecideAllocatesFirstMatchingCandidate(t *testing.T) {
	t.Parallel()

	result := Decide(testCommand(t))

	allocated, ok := result.(Allocated)
	if !ok {
		t.Fatalf("Decide() result = %T, want Allocated", result)
	}
	if allocated.Host.Name != "host-a" {
		t.Fatalf("allocated host = %q, want host-a", allocated.Host.Name)
	}
}

func TestDecideReturnsUnsupportedCapabilityWhenOnlyCapabilityBlocksAllocation(t *testing.T) {
	t.Parallel()

	command := testCommand(t)
	powerOnOnly, err := capability.NewSet(capability.PowerOn)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	command.Candidates = []Candidate{
		replaceCandidate(command.Candidates[0], func(candidate *Candidate) {
			candidate.Capabilities = powerOnOnly
		}),
	}

	result := Decide(command)

	notAllocated, ok := result.(NotAllocated)
	if !ok {
		t.Fatalf("Decide() result = %T, want NotAllocated", result)
	}
	failure, ok := notAllocated.Failure.(UnsupportedCapability)
	if !ok {
		t.Fatalf("failure = %T, want UnsupportedCapability", notAllocated.Failure)
	}
	if !slices.Equal(failure.Missing, []capability.Capability{capability.SetNextBoot}) {
		t.Fatalf("missing capabilities = %v, want [SetNextBoot]", failure.Missing)
	}
}

func TestDecideReturnsNoMatchingHostWhenCandidateDimensionsDoNotMatch(t *testing.T) {
	t.Parallel()

	command := testCommand(t)
	command.Candidates = []Candidate{
		replaceCandidate(command.Candidates[0], func(candidate *Candidate) {
			candidate.PlatformProfile = "arm64-uefi-ab/v1"
		}),
	}

	result := Decide(command)

	notAllocated, ok := result.(NotAllocated)
	if !ok {
		t.Fatalf("Decide() result = %T, want NotAllocated", result)
	}
	if _, ok := notAllocated.Failure.(NoMatchingHost); !ok {
		t.Fatalf("failure = %T, want NoMatchingHost", notAllocated.Failure)
	}
}

func TestDecideKeepsClaimedHostForSameMachine(t *testing.T) {
	t.Parallel()

	command := testCommand(t)
	command.Candidates = []Candidate{
		replaceCandidate(command.Candidates[0], func(candidate *Candidate) {
			candidate.Phase = hostdomain.PhaseReserved
			candidate.Assignment = AssignedToMachine{Machine: command.Machine}
		}),
	}

	if _, ok := Decide(command).(Allocated); !ok {
		t.Fatalf("Decide() result = %T, want Allocated", Decide(command))
	}
}

func testCommand(t *testing.T) Command {
	t.Helper()

	requirements, err := NewRequirements(
		"amd64",
		"UEFI",
		"amd64-uefi-ab/v1",
		256_000_000_000,
		[]capability.Capability{capability.PowerOn, capability.SetNextBoot},
		map[string]string{"rack": "a"},
	)
	if err != nil {
		t.Fatalf("NewRequirements() error = %v", err)
	}
	allCapabilities, err := capability.NewSet(capability.PowerOn, capability.SetNextBoot)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	return Command{
		Machine: MachineRef{
			Namespace: "default",
			Name:      "machine-a",
			UID:       "machine-a-uid",
		},
		Requirements: requirements,
		Candidates: []Candidate{
			{
				Host: HostRef{
					Namespace: "default",
					Name:      "host-a",
					UID:       "host-a-uid",
				},
				Phase:             hostdomain.PhaseAvailable,
				Assignment:        Unassigned{},
				Architecture:      "amd64",
				Firmware:          "UEFI",
				PlatformProfile:   "amd64-uefi-ab/v1",
				RootDiskSizeBytes: 512_000_000_000,
				Capabilities:      allCapabilities,
				Labels:            map[string]string{"rack": "a"},
			},
		},
	}
}

func replaceCandidate(candidate Candidate, mutate func(*Candidate)) Candidate {
	mutate(&candidate)
	return candidate
}
