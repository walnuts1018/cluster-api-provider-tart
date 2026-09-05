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

package bootstrap

import (
	"bytes"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestValidateConfigSecret(t *testing.T) {
	t.Parallel()

	immutable := true
	tests := []struct {
		name   string
		secret *corev1.Secret
		want   error
	}{
		{name: "nil", want: ErrMissingConfigSecret},
		{name: "mutable", secret: &corev1.Secret{Name: "config", Data: map[string][]byte{"patch.yaml": []byte("machine: {}")}}, want: ErrConfigSecretNotImmutable},
		{name: "empty", secret: &corev1.Secret{Name: "config", Immutable: &immutable}, want: ErrConfigSecretEmpty},
		{name: "only empty values", secret: &corev1.Secret{Name: "config", Immutable: &immutable, Data: map[string][]byte{"patch.yaml": nil}}, want: ErrConfigSecretEmpty},
		{name: "valid", secret: &corev1.Secret{Name: "config", Immutable: &immutable, Data: map[string][]byte{"patch.yaml": []byte("machine: {}")}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateConfigSecret(tt.secret)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("ValidateConfigSecret() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("ValidateConfigSecret() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBuildSecret(t *testing.T) {
	t.Parallel()

	ownerUID := types.UID("config-uid")
	configuration := []byte("machine:\n  type: worker\n")
	secret, err := BuildSecret("cluster-a", "bootstrap-a", "cluster-a", metav1.OwnerReference{
		APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha1",
		Kind:       "TartBootstrapConfig",
		Name:       "bootstrap-a",
		UID:        ownerUID,
	}, configuration)
	if err != nil {
		t.Fatalf("BuildSecret() error = %v", err)
	}
	if !IsContractSecret(secret, "cluster-a", ownerUID) {
		t.Fatal("BuildSecret() produced a Secret outside the Bootstrap contract")
	}
	configuration[0] = 'X'
	if bytes.Equal(secret.Data[BootstrapSecretKey], configuration) {
		t.Fatal("BuildSecret() retained the caller's mutable byte slice")
	}
	if !bytes.Equal(secret.Data[BootstrapSecretKey], []byte("machine:\n  type: worker\n")) {
		t.Errorf("value = %q, want complete configuration", secret.Data[BootstrapSecretKey])
	}
}
