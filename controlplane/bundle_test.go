package controlplane

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestBundleNameAndPendingSecret(t *testing.T) {
	t.Parallel()

	owner := metav1.OwnerReference{
		APIVersion: infrav1alpha1.GroupVersion.String(),
		Kind:       "TartCluster",
		Name:       "cluster-a",
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
	wrongOwner := owner
	wrongOwner.Kind = "TartControlPlane"
	if _, err := BuildPendingSecret("cluster-a", "cluster-a", "018f3c5e-5f8a-7c1b-9a2d-123456789abc", 2, wrongOwner, data); !errors.Is(err, ErrBundleOwnerInvalid) {
		t.Fatalf("BuildPendingSecret() error = %v, want ErrBundleOwnerInvalid", err)
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

func TestValidateBundleSecretContract(t *testing.T) {
	t.Parallel()

	owner := metav1.OwnerReference{
		APIVersion: infrav1alpha1.GroupVersion.String(),
		Kind:       "TartCluster",
		Name:       "cluster-a",
		UID:        types.UID("cluster-uid"),
	}
	secret, err := BuildPendingSecret("ns", "cluster-a", "018f3c5e-5f8a-7c1b-9a2d-123456789abc", 2, owner, map[string][]byte{"talos.ca": []byte("ca")})
	if err != nil {
		t.Fatalf("BuildPendingSecret() error = %v", err)
	}

	if err := ValidateBundleSecretContract(secret, "ns", "cluster-a", "018f3c5e-5f8a-7c1b-9a2d-123456789abc", 2, BundleStatePending, owner.UID); err != nil {
		t.Fatalf("ValidateBundleSecretContract() error = %v", err)
	}

	tests := map[string]func(*corev1.Secret){
		"wrong cluster ID label": func(secret *corev1.Secret) {
			secret.Labels[ClusterIDLabel] = "018f3c5e-5f8a-7c1b-9a2d-123456789abd"
		},
		"mutable Secret": func(secret *corev1.Secret) {
			secret.Immutable = new(false)
		},
		"wrong owner kind": func(secret *corev1.Secret) {
			secret.OwnerReferences[0].Kind = "TartControlPlane"
		},
		"missing bundle data": func(secret *corev1.Secret) {
			secret.Data = nil
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			invalid := secret.DeepCopy()
			mutate(invalid)
			if err := ValidateBundleSecretContract(invalid, "ns", "cluster-a", "018f3c5e-5f8a-7c1b-9a2d-123456789abc", 2, BundleStatePending, owner.UID); !errors.Is(err, ErrBundleSecretInvalid) {
				t.Fatalf("ValidateBundleSecretContract() error = %v, want ErrBundleSecretInvalid", err)
			}
		})
	}
}

func TestBundleNameRejectsNameThatExceedsDNSLimit(t *testing.T) {
	t.Parallel()

	clusterName := strings.Repeat("a", 240)
	if _, err := BundleName(clusterName, "018f3c5e-5f8a-7c1b-9a2d-123456789abc", 1); !errors.Is(err, ErrInvalidClusterIdentity) {
		t.Fatalf("BundleName() error = %v, want ErrInvalidClusterIdentity", err)
	}
}
