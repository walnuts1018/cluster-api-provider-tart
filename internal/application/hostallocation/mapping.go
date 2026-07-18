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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/platformprofile"
)

func CommandFromMachine(machine *infrastructurev1beta1.TartMachine) (domain.Command, error) {
	requirements, err := RequirementsForMachine(machine)
	if err != nil {
		return domain.Command{}, err
	}
	return domain.Command{
		Machine: domain.MachineRef{
			Namespace: machine.Namespace,
			Name:      machine.Name,
			UID:       string(machine.UID),
		},
		Requirements: requirements,
	}, nil
}

func RequirementsForMachine(machine *infrastructurev1beta1.TartMachine) (domain.Requirements, error) {
	architecture, firmware, err := parsePlatformProfile(machine.Spec.PlatformProfile)
	if err != nil {
		return domain.Requirements{}, err
	}

	const minRootDiskBytes int64 = 64 * 1024 * 1024 * 1024
	return domain.NewRequirements(
		architecture,
		firmware,
		machine.Spec.PlatformProfile,
		minRootDiskBytes,
		[]capability.Capability{capability.PowerOn},
		machine.Spec.HostSelector.MatchLabels,
	)
}

func parsePlatformProfile(profile string) (architecture, firmware string, err error) {
	definition, ok := platformprofile.Lookup(profile)
	if !ok {
		return "", "", fmt.Errorf("unsupported platform profile %q", profile)
	}
	return definition.Architecture, definition.Firmware, nil
}
