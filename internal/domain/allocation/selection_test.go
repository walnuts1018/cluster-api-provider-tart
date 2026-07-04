package allocation

import (
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

func TestMatch(t *testing.T) {
	t.Parallel()

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
		t.Fatalf("capability.NewSet() error = %v", err)
	}
	powerOnOnly, err := capability.NewSet(capability.PowerOn)
	if err != nil {
		t.Fatalf("capability.NewSet() error = %v", err)
	}

	matching := Candidate{
		Phase:             hostdomain.PhaseAvailable,
		Architecture:      "amd64",
		Firmware:          "UEFI",
		PlatformProfile:   "amd64-uefi-ab/v1",
		RootDiskSizeBytes: 512_000_000_000,
		Capabilities:      allCapabilities,
		Labels:            map[string]string{"rack": "a", "zone": "lab"},
	}

	tests := []struct {
		name      string
		candidate Candidate
		want      Mismatch
	}{
		{name: "一致", candidate: matching, want: MismatchNone},
		{name: "利用可能でない", candidate: replace(matching, func(candidate *Candidate) {
			candidate.Phase = hostdomain.PhaseReserved
		}), want: MismatchPhase},
		{name: "割り当て済み", candidate: replace(matching, func(candidate *Candidate) {
			candidate.Assigned = true
		}), want: MismatchAssigned},
		{name: "アーキテクチャ不一致", candidate: replace(matching, func(candidate *Candidate) {
			candidate.Architecture = "arm64"
		}), want: MismatchArchitecture},
		{name: "ファームウェア不一致", candidate: replace(matching, func(candidate *Candidate) {
			candidate.Firmware = "LegacyBIOS"
		}), want: MismatchFirmware},
		{name: "プロファイル不一致", candidate: replace(matching, func(candidate *Candidate) {
			candidate.PlatformProfile = "amd64-uefi-ab/v2"
		}), want: MismatchPlatformProfile},
		{name: "ディスク容量不足", candidate: replace(matching, func(candidate *Candidate) {
			candidate.RootDiskSizeBytes = 255_999_999_999
		}), want: MismatchRootDiskSize},
		{name: "Capability不足", candidate: replace(matching, func(candidate *Candidate) {
			candidate.Capabilities = powerOnOnly
		}), want: MismatchCapability},
		{name: "ラベル不一致", candidate: replace(matching, func(candidate *Candidate) {
			candidate.Labels = map[string]string{"rack": "b"}
		}), want: MismatchLabel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Match(requirements, tt.candidate); got != tt.want {
				t.Fatalf("Match() = %q, want %q", got, tt.want)
			}
		})
	}
}

func replace(candidate Candidate, change func(*Candidate)) Candidate {
	change(&candidate)
	return candidate
}
