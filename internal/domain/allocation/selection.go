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

package allocation

import (
	"errors"
	"fmt"
	"maps"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

// ErrNoMatchingHost は割当要件を満たす未割当Hostが存在しないことを表す。
var ErrNoMatchingHost = errors.New("no matching TartHost")

type Requirements struct {
	Architecture     string
	Firmware         string
	PlatformProfile  string
	MinRootDiskBytes int64
	Capabilities     capability.Set
	MatchLabels      map[string]string
}

type Candidate struct {
	Phase             hostdomain.Phase
	Assigned          bool
	Architecture      string
	Firmware          string
	PlatformProfile   string
	RootDiskSizeBytes int64
	Capabilities      capability.Set
	Labels            map[string]string
}

type Mismatch string

const (
	MismatchNone            Mismatch = ""
	MismatchPhase           Mismatch = "Phase"
	MismatchAssigned        Mismatch = "Assigned"
	MismatchArchitecture    Mismatch = "Architecture"
	MismatchFirmware        Mismatch = "Firmware"
	MismatchPlatformProfile Mismatch = "PlatformProfile"
	MismatchRootDiskSize    Mismatch = "RootDiskSize"
	MismatchCapability      Mismatch = "Capability"
	MismatchLabel           Mismatch = "Label"
)

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

func Match(requirements Requirements, candidate Candidate) Mismatch {
	switch {
	case candidate.Phase != hostdomain.PhaseAvailable:
		return MismatchPhase
	case candidate.Assigned:
		return MismatchAssigned
	case candidate.Architecture != requirements.Architecture:
		return MismatchArchitecture
	case candidate.Firmware != requirements.Firmware:
		return MismatchFirmware
	case candidate.PlatformProfile != requirements.PlatformProfile:
		return MismatchPlatformProfile
	case candidate.RootDiskSizeBytes < requirements.MinRootDiskBytes:
		return MismatchRootDiskSize
	case !candidate.Capabilities.ContainsAll(requirements.Capabilities):
		return MismatchCapability
	case !containsLabels(candidate.Labels, requirements.MatchLabels):
		return MismatchLabel
	}
	return MismatchNone
}

func containsLabels(actual, required map[string]string) bool {
	for key, value := range required {
		if actual[key] != value {
			return false
		}
	}
	return true
}
