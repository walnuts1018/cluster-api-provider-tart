package allocation

import (
	"fmt"
	"maps"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
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
