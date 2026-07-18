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
	"strings"
	"testing"
)

func TestAllは要求されたOSDistribution組み合わせを含む(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		ProfileUbuntu2404Kubeadm: {},
		ProfileUbuntu2404K3s:     {},
		ProfileUbuntu2604Kubeadm: {},
		ProfileUbuntu2604K3s:     {},
		ProfileDebian13Kubeadm:   {},
		ProfileDebian13K3s:       {},
	}
	for _, profile := range All() {
		delete(want, profile.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing profiles: %#v", want)
	}
}

func TestValidateArtifactIdentityはProfile外の組み合わせを拒否する(t *testing.T) {
	t.Parallel()

	profile, err := MustLookup(ProfileUbuntu2604K3s)
	if err != nil {
		t.Fatalf("MustLookup() error = %v", err)
	}
	valid := ArtifactIdentity{
		OSFamily:          "ubuntu",
		OSVersion:         "26.04",
		Architecture:      ArchitectureAMD64,
		Distribution:      DistributionK3s,
		KubernetesVersion: "v1.36.2",
		CPULevel:          "x86-64-v1",
		StateSchemaMin:    1,
		StateSchemaMax:    1,
	}
	if err := ValidateArtifactIdentity(profile, valid); err != nil {
		t.Fatalf("ValidateArtifactIdentity() error = %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*ArtifactIdentity)
		wantErr string
	}{
		{name: "OS family", mutate: func(identity *ArtifactIdentity) { identity.OSFamily = "debian" }, wantErr: "os.family"},
		{name: "OS version", mutate: func(identity *ArtifactIdentity) { identity.OSVersion = "24.04" }, wantErr: "os.version"},
		{name: "distribution", mutate: func(identity *ArtifactIdentity) { identity.Distribution = DistributionKubeadm }, wantErr: "distribution"},
		{name: "kubernetes version", mutate: func(identity *ArtifactIdentity) {
			identity.KubernetesVersion = "v1.35.0"
		}, wantErr: "kubernetes.version"},
		{name: "state schema", mutate: func(identity *ArtifactIdentity) { identity.StateSchemaMax = 0 }, wantErr: "stateSchema"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			identity := valid
			tt.mutate(&identity)
			err := ValidateArtifactIdentity(profile, identity)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateArtifactIdentity() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
