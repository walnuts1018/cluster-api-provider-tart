package recovery

import (
	"crypto/x509"
	"encoding/pem"
	"slices"
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	corev1 "k8s.io/api/core/v1"

	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
)

func testMaterial(t *testing.T) Material {
	t.Helper()
	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	return Material{
		ClusterID:            clusterdomain.NewClusterID().String(),
		CertificateAuthority: bundle.Certs.OS,
	}
}

// TestRecoverySecretRoundTripは、recovery Secretが最小限のTalos API PKIだけを保持し、同じcluster identityとして復号できることを確認する。
func TestRecoverySecretRoundTrip(t *testing.T) {
	t.Parallel()

	material := testMaterial(t)
	secret, err := BuildSecret("tart-system", material)
	if err != nil {
		t.Fatalf("BuildSecret() error = %v", err)
	}
	if secret.Immutable == nil || !*secret.Immutable {
		t.Fatal("BuildSecret() must produce an immutable Secret")
	}
	if len(secret.OwnerReferences) != 0 {
		t.Fatal("BuildSecret() must not set an OwnerReference; lifetime is decided by TartHost references")
	}
	wantKeys := []string{CACertificateKey, CAKeyKey, ClusterIDKey}
	for key := range secret.Data {
		if !slices.Contains(wantKeys, key) {
			t.Fatalf("BuildSecret() stored unexpected key %q; Kubernetes PKI and Bootstrap Data must not be copied", key)
		}
	}

	decoded, err := DecodeSecret(secret, material.ClusterID)
	if err != nil {
		t.Fatalf("DecodeSecret() error = %v", err)
	}
	if decoded.ClusterID != material.ClusterID {
		t.Fatalf("DecodeSecret() clusterID = %q, want %q", decoded.ClusterID, material.ClusterID)
	}

	if _, err := DecodeSecret(secret, clusterdomain.NewClusterID().String()); err == nil {
		t.Fatal("DecodeSecret() must reject a recovery Secret from a different Talos cluster identity")
	}

	unlabeled := secret.DeepCopy()
	unlabeled.Labels = nil
	if _, err := DecodeSecret(unlabeled, material.ClusterID); err == nil {
		t.Fatal("DecodeSecret() must reject a Secret that is not labeled as a recovery Secret")
	}

	mutable := secret.DeepCopy()
	mutable.Immutable = nil
	if _, err := DecodeSecret(mutable, material.ClusterID); err == nil {
		t.Fatal("DecodeSecret() must reject a mutable recovery Secret")
	}

	renamed := secret.DeepCopy()
	renamed.Name = "tart-talos-recovery-other"
	if _, err := DecodeSecret(renamed, material.ClusterID); err == nil {
		t.Fatal("DecodeSecret() must reject a recovery Secret whose name does not match its cluster identity")
	}

	if secret.Type != corev1.SecretTypeOpaque {
		t.Fatalf("BuildSecret() type = %q", secret.Type)
	}
}

// TestClientCertificateIsShortLivedAdminは、Reset RPCへ必要な`os:admin` roleの証明書が都度発行され、短命に制限されることを確認する。
func TestClientCertificateIsShortLivedAdmin(t *testing.T) {
	t.Parallel()

	material := testMaterial(t)
	certificate, err := material.ClientCertificate(ClientCertificateTTL)
	if err != nil {
		t.Fatalf("ClientCertificate() error = %v", err)
	}
	block, _ := pem.Decode(certificate.Crt)
	if block == nil {
		t.Fatal("ClientCertificate() did not return a PEM encoded certificate")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate() error = %v", err)
	}
	if lifetime := parsed.NotAfter.Sub(parsed.NotBefore); lifetime > ClientCertificateTTL+time.Minute {
		t.Fatalf("ClientCertificate() lifetime = %s, want at most %s", lifetime, ClientCertificateTTL)
	}
	if !slices.Contains(parsed.Subject.Organization, "os:admin") {
		t.Fatalf("ClientCertificate() organizations = %v, want os:admin", parsed.Subject.Organization)
	}

	// 過大なTTLを要求してもClientCertificateTTLへ丸め込み、常設のadmin credentialにしない。
	long, err := material.ClientCertificate(24 * time.Hour)
	if err != nil {
		t.Fatalf("ClientCertificate() error = %v", err)
	}
	longBlock, _ := pem.Decode(long.Crt)
	longParsed, err := x509.ParseCertificate(longBlock.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate() error = %v", err)
	}
	if lifetime := longParsed.NotAfter.Sub(longParsed.NotBefore); lifetime > ClientCertificateTTL+time.Minute {
		t.Fatalf("ClientCertificate() clamped lifetime = %s, want at most %s", lifetime, ClientCertificateTTL)
	}

	if _, err := (Material{ClusterID: material.ClusterID}).ClientCertificate(ClientCertificateTTL); err == nil {
		t.Fatal("ClientCertificate() must fail without recovery certificate authority material")
	}
}
