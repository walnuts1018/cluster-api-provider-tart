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
	"maps"

	capability "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/host"
)

type Requirements struct {
	Architecture     string
	Firmware         string
	PlatformProfile  string
	MinRootDiskBytes int64
	Capabilities     capability.Set
	MatchLabels      map[string]string
}

type Candidate struct {
	Host              HostRef
	Phase             hostdomain.Phase
	Assignment        Assignment
	Architecture      string
	Firmware          string
	PlatformProfile   string
	RootDiskSizeBytes int64
	Capabilities      capability.Set
	Labels            map[string]string
}

// go-sumtype:decl Assignment
type Assignment interface {
	isAssignment()
}

type Unassigned struct{}

type AssignedToMachine struct {
	Machine MachineRef
}

func (Unassigned) isAssignment()        {}
func (AssignedToMachine) isAssignment() {}

func NewRequirements(
	architecture string,
	firmware string,
	platformProfile string,
	minRootDiskBytes int64,
	requiredCapabilities []capability.Capability,
	matchLabels map[string]string,
) (Requirements, error) {
	if architecture == "" {
		return Requirements{}, fmt.Errorf("architecture must not be empty")
	}
	if firmware == "" {
		return Requirements{}, fmt.Errorf("firmware must not be empty")
	}
	if platformProfile == "" {
		return Requirements{}, fmt.Errorf("platform profile must not be empty")
	}
	if minRootDiskBytes <= 0 {
		return Requirements{}, fmt.Errorf("minimum root disk size must be positive")
	}
	capabilities, err := capability.NewSet(requiredCapabilities...)
	if err != nil {
		return Requirements{}, err
	}
	return Requirements{
		Architecture:     architecture,
		Firmware:         firmware,
		PlatformProfile:  platformProfile,
		MinRootDiskBytes: minRootDiskBytes,
		Capabilities:     capabilities,
		MatchLabels:      maps.Clone(matchLabels),
	}, nil
}
