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

package controlplane

import (
	"bytes"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBundleNameAndPendingSecret(t *testing.T) {
	t.Parallel()

	owner := metav1.OwnerReference{
		APIVersion: "controlplane.cluster.x-k8s.io/v1alpha1",
		Kind:       "TartControlPlane",
		Name:       "control-plane",
		UID:        types.UID("control-plane-uid"),
	}
	data := map[string][]byte{"talos.ca": []byte("ca"), "kubernetes.ca": []byte("kube-ca")}
	secret, err := BuildPendingSecret("cluster-a", "cluster-a", "018f3c5e-5f8a-7c1b-9a2d-123456789abc", 2, owner, data)
	if err != nil {
		t.Fatalf("BuildPendingSecret() error = %v", err)
	}
	if secret.Name != "cluster-a-talos-secrets-018f3c5e-5f8a-7c1b-9a2d-123456789abc-g2" {
		t.Errorf("name = %q, want deterministic generation name", secret.Name)
	}
	if secret.Immutable == nil || !*secret.Immutable {
		t.Fatal("pending bundle Secret must be immutable")
	}
	if secret.Labels[BundleStateLabel] != BundleStatePending {
		t.Errorf("bundle state = %q, want %q", secret.Labels[BundleStateLabel], BundleStatePending)
	}
	data["talos.ca"][0] = 'X'
	if bytes.Equal(secret.Data["talos.ca"], data["talos.ca"]) {
		t.Fatal("BuildPendingSecret() retained caller-owned mutable bytes")
	}
}

func TestNextGeneration(t *testing.T) {
	t.Parallel()

	if got, err := NextGeneration(0); err != nil || got != 1 {
		t.Errorf("NextGeneration(0) = %d, %v, want 1", got, err)
	}
	if got, err := NextGeneration(3); err != nil || got != 4 {
		t.Errorf("NextGeneration(3) = %d, %v, want 4", got, err)
	}
	if _, err := NextGeneration(-1); !errors.Is(err, ErrInvalidBundleGeneration) {
		t.Errorf("NextGeneration(-1) error = %v, want ErrInvalidBundleGeneration", err)
	}
}

func TestRotateDataPreservesUntargetedMaterial(t *testing.T) {
	t.Parallel()

	previous := map[string][]byte{
		"talos.ca":        []byte("old-tal os ca"),
		"kubernetes.ca":   []byte("old-kubernetes ca"),
		"etcd.ca":         []byte("etcd ca"),
		"service-account": []byte("service account key"),
	}
	rotated, err := RotateData(previous, map[string][]byte{
		"talos.ca":      []byte("new-talos-ca"),
		"kubernetes.ca": []byte("new-kubernetes-ca"),
	}, []string{"talos.ca", "kubernetes.ca"})
	if err != nil {
		t.Fatalf("RotateData() error = %v", err)
	}
	if string(rotated["talos.ca"]) != "new-talos-ca" || string(rotated["kubernetes.ca"]) != "new-kubernetes-ca" {
		t.Fatalf("rotation targets were not replaced: %#v", rotated)
	}
	if string(rotated["etcd.ca"]) != string(previous["etcd.ca"]) || string(rotated["service-account"]) != string(previous["service-account"]) {
		t.Fatal("rotation changed material outside the requested target set")
	}
	rotated["etcd.ca"][0] = 'X'
	if bytes.Equal(rotated["etcd.ca"], previous["etcd.ca"]) {
		t.Fatal("RotateData() retained previous bundle byte slices")
	}
}

func TestRotateDataRejectsPartialReplacement(t *testing.T) {
	t.Parallel()

	_, err := RotateData(map[string][]byte{"talos.ca": []byte("old")}, map[string][]byte{}, []string{"talos.ca"})
	if !errors.Is(err, ErrRotationTargetMismatch) {
		t.Errorf("RotateData() error = %v, want ErrRotationTargetMismatch", err)
	}
}
