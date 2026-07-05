package disk

import (
	"errors"
	"fmt"
	"slices"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

var (
	ErrDiskIdentityMismatch   = errors.New("DiskIdentityMismatch")
	ErrDiskSelectionAmbiguous = errors.New("DiskSelectionAmbiguous")
)

type Device struct {
	Path         string
	ByIDPaths    []string
	SerialNumber string
	WWN          string
	SizeBytes    int64
	HoldsAgentOS bool
}

// SelectはPlanの全disk identity条件を満たす、Agent自身の起動元ではない唯一のdiskだけを返す。
func Select(expected agentprotocol.RootDevice, devices []Device) (Device, error) {
	matches := make([]Device, 0, 1)
	for _, device := range devices {
		if matchesIdentity(expected, device) {
			matches = append(matches, device)
		}
	}

	switch len(matches) {
	case 0:
		return Device{}, fmt.Errorf("%w: no disk satisfies every root device constraint", ErrDiskIdentityMismatch)
	case 1:
		return matches[0], nil
	default:
		return Device{}, fmt.Errorf("%w: %d disks satisfy every root device constraint", ErrDiskSelectionAmbiguous, len(matches))
	}
}

func matchesIdentity(expected agentprotocol.RootDevice, device Device) bool {
	return slices.Contains(device.ByIDPaths, expected.DeviceName) &&
		(expected.SerialNumber == "" || device.SerialNumber == expected.SerialNumber) &&
		(expected.WWN == "" || device.WWN == expected.WWN) &&
		device.SizeBytes >= expected.MinSizeBytes &&
		!device.HoldsAgentOS
}
