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

package platformprofile

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	ArchitectureAMD64 = "amd64"
	FirmwareUEFI      = "UEFI"
	BootDriverIPXE    = "ipxe"

	DistributionKubeadm = "kubeadm"
	DistributionK3s     = "k3s"
	DistributionK0s     = "k0s"

	KubernetesV136 = "v1.36.x"

	ProfileUbuntu2404Kubeadm = "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1"
	ProfileUbuntu2404K3s     = "amd64-uefi-ab-ubuntu-24.04-k3s/v1"
	ProfileUbuntu2404K0s     = "amd64-uefi-ab-ubuntu-24.04-k0s/v1"
	ProfileUbuntu2604Kubeadm = "amd64-uefi-ab-ubuntu-26.04-kubeadm/v1"
	ProfileUbuntu2604K3s     = "amd64-uefi-ab-ubuntu-26.04-k3s/v1"
	ProfileUbuntu2604K0s     = "amd64-uefi-ab-ubuntu-26.04-k0s/v1"
	ProfileDebian13Kubeadm   = "amd64-uefi-ab-debian-13-kubeadm/v1"
	ProfileDebian13K3s       = "amd64-uefi-ab-debian-13-k3s/v1"
	ProfileDebian13K0s       = "amd64-uefi-ab-debian-13-k0s/v1"

	LegacyProfileAMD64UEFIABV1 = "amd64-uefi-ab/v1"
)

var ErrUnsupportedProfile = errors.New("unsupported platform profile")

type Profile struct {
	ID                 string
	Architecture       string
	Firmware           string
	BootDriver         string
	OSFamily           string
	OSVersion          string
	Distribution       string
	KubernetesVersions []string
	CPULevel           string
	StateSchema        uint64
	StatePaths         []string
	DataPaths          []string
}

type ArtifactIdentity struct {
	OSFamily          string
	OSVersion         string
	Architecture      string
	Distribution      string
	KubernetesVersion string
	CPULevel          string
	StateSchemaMin    uint64
	StateSchemaMax    uint64
}

type osTarget struct {
	family  string
	version string
	slug    string
}

var amd64UEFIABOSTargets = []osTarget{
	{family: "ubuntu", version: "24.04", slug: "ubuntu-24.04"},
	{family: "ubuntu", version: "26.04", slug: "ubuntu-26.04"},
	{family: "debian", version: "13", slug: "debian-13"},
}

var amd64UEFIABDistributions = []string{
	DistributionKubeadm,
	DistributionK3s,
	DistributionK0s,
}

var profiles = buildProfiles()

func buildProfiles() []Profile {
	result := make([]Profile, 0, len(amd64UEFIABOSTargets)*len(amd64UEFIABDistributions)+1)
	for _, target := range amd64UEFIABOSTargets {
		for _, distribution := range amd64UEFIABDistributions {
			id := fmt.Sprintf("amd64-uefi-ab-%s-%s/v1", target.slug, distribution)
			result = append(result, newAMD64UEFIABProfile(id, target.family, target.version, distribution))
		}
	}
	result = append(result, newAMD64UEFIABProfile(LegacyProfileAMD64UEFIABV1, "ubuntu", "24.04", DistributionKubeadm))
	return result
}

func newAMD64UEFIABProfile(id, osFamily, osVersion, distribution string) Profile {
	profile := Profile{
		ID:                 id,
		Architecture:       ArchitectureAMD64,
		Firmware:           FirmwareUEFI,
		BootDriver:         BootDriverIPXE,
		OSFamily:           osFamily,
		OSVersion:          osVersion,
		Distribution:       distribution,
		KubernetesVersions: []string{KubernetesV136},
		CPULevel:           "x86-64-v1",
		StateSchema:        1,
		StatePaths: []string{
			"/etc/machine-id",
			"/etc/tart",
		},
		DataPaths: []string{
			"/var/lib/containerd",
			"/var/lib/kubelet",
		},
	}
	switch distribution {
	case DistributionKubeadm:
		profile.StatePaths = append(profile.StatePaths, "/etc/kubernetes")
		profile.DataPaths = append(profile.DataPaths, "/var/lib/etcd")
	case DistributionK3s:
		profile.StatePaths = append(profile.StatePaths, "/etc/rancher/k3s")
		profile.DataPaths = append(profile.DataPaths, "/var/lib/rancher/k3s")
	case DistributionK0s:
		profile.StatePaths = append(profile.StatePaths, "/etc/k0s")
		profile.DataPaths = append(profile.DataPaths, "/var/lib/k0s")
	}
	return profile
}

func All() []Profile {
	return slices.Clone(profiles)
}

func Lookup(id string) (Profile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile.clone(), true
		}
	}
	return Profile{}, false
}

func MustLookup(id string) (Profile, error) {
	profile, ok := Lookup(id)
	if !ok {
		return Profile{}, fmt.Errorf("%w: %q", ErrUnsupportedProfile, id)
	}
	return profile, nil
}

func ValidateArtifactIdentity(profile Profile, identity ArtifactIdentity) error {
	switch {
	case identity.OSFamily != profile.OSFamily:
		return fmt.Errorf("artifact os.family %q does not match platform profile %q", identity.OSFamily, profile.ID)
	case identity.OSVersion != profile.OSVersion:
		return fmt.Errorf("artifact os.version %q does not match platform profile %q", identity.OSVersion, profile.ID)
	case identity.Architecture != profile.Architecture:
		return fmt.Errorf("artifact architecture %q does not match platform profile %q", identity.Architecture, profile.ID)
	case identity.Distribution != profile.Distribution:
		return fmt.Errorf("artifact kubernetes.distribution %q does not match platform profile %q", identity.Distribution, profile.ID)
	case !supportsKubernetesVersion(profile.KubernetesVersions, identity.KubernetesVersion):
		return fmt.Errorf("artifact kubernetes.version %q is not supported by platform profile %q", identity.KubernetesVersion, profile.ID)
	case identity.CPULevel != profile.CPULevel:
		return fmt.Errorf("artifact requirements.cpuLevel %q does not match platform profile %q", identity.CPULevel, profile.ID)
	case identity.StateSchemaMin == 0 || identity.StateSchemaMax < profile.StateSchema || identity.StateSchemaMin > profile.StateSchema:
		return fmt.Errorf("artifact stateSchema range does not include platform profile %q state schema", profile.ID)
	}
	return nil
}

func supportsKubernetesVersion(supportedVersions []string, actual string) bool {
	for _, supported := range supportedVersions {
		if supported == actual {
			return true
		}
		if strings.HasSuffix(supported, ".x") && hasPatchVersion(strings.TrimSuffix(supported, ".x"), actual) {
			return true
		}
	}
	return false
}

func hasPatchVersion(minor, actual string) bool {
	prefix := minor + "."
	if !strings.HasPrefix(actual, prefix) {
		return false
	}
	patch := strings.TrimPrefix(actual, prefix)
	if patch == "" {
		return false
	}
	_, err := strconv.ParseUint(patch, 10, 64)
	return err == nil
}

func (profile Profile) clone() Profile {
	profile.KubernetesVersions = slices.Clone(profile.KubernetesVersions)
	profile.StatePaths = slices.Clone(profile.StatePaths)
	profile.DataPaths = slices.Clone(profile.DataPaths)
	return profile
}
