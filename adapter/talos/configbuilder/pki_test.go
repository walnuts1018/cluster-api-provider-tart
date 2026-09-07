package configbuilder

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	siderox509 "github.com/siderolabs/crypto/x509"
)

func generateTestCertificate(t *testing.T, commonName string) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:         true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("pem encode: %v", err)
	}
	return buf.Bytes()
}

func generateTestPrivateKey(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		t.Fatalf("pem encode: %v", err)
	}
	return buf.Bytes()
}

func TestCertificateFingerprint_ValidAndMalformed(t *testing.T) {
	t.Parallel()
	valid := generateTestCertificate(t, "test-ca")
	if _, err := certificateFingerprint(valid); err != nil {
		t.Fatalf("valid certificate should not error: %v", err)
	}
	if _, err := certificateFingerprint([]byte("not a pem")); err == nil {
		t.Fatal("malformed PEM should error")
	}
	if _, err := certificateFingerprint([]byte("")); err == nil {
		t.Fatal("empty should error")
	}
	// PEM with trailing data should error
	trailing := append(valid, []byte("trailing")...)
	if _, err := certificateFingerprint(trailing); err == nil {
		t.Fatal("trailing data should error")
	}
}

func TestCertificateFingerprint_PEMFormattingInsensitive(t *testing.T) {
	t.Parallel()
	cert := generateTestCertificate(t, "same-ca")
	// Add extra newlines and spaces – trimmed handling should still succeed if block is valid?
	// Our implementation trims outer space but requires no trailing data after first block, so extra newlines after should be trimmed and succeed.
	withNewlines := append([]byte("\n"), cert...)
	withNewlines = append(withNewlines, []byte("\n\n")...)
	fp1, err := certificateFingerprint(cert)
	if err != nil {
		t.Fatalf("fp1 error: %v", err)
	}
	fp2, err := certificateFingerprint(withNewlines)
	if err != nil {
		t.Fatalf("fp2 error: %v", err)
	}
	if fp1 != fp2 {
		t.Fatal("formatting difference should yield same fingerprint")
	}
}

func TestEqualCertificateSet_NilVsEmpty(t *testing.T) {
	t.Parallel()
	empty := []*siderox509.PEMEncodedCertificate{}
	var nilSlice []*siderox509.PEMEncodedCertificate
	if !equalCertificateSet(empty, nilSlice) {
		t.Fatal("empty vs nil should be equal")
	}
	nilElement := []*siderox509.PEMEncodedCertificate{nil}
	if equalCertificateSet(nilElement, empty) {
		t.Fatal("[nil] vs [] should not be equal (fail-open)")
	}
	if equalCertificateSet(nilElement, nilSlice) {
		t.Fatal("[nil] vs nil should not be equal")
	}
}

func TestEqualCertificateSet_OrderInsensitiveAndDuplicate(t *testing.T) {
	t.Parallel()
	certA := generateTestCertificate(t, "ca-a")
	certB := generateTestCertificate(t, "ca-b")
	a := &siderox509.PEMEncodedCertificate{Crt: certA}
	b := &siderox509.PEMEncodedCertificate{Crt: certB}
	if !equalCertificateSet([]*siderox509.PEMEncodedCertificate{a, b}, []*siderox509.PEMEncodedCertificate{b, a}) {
		t.Fatal("order should not matter")
	}
	// Duplicate should be set semantics – [A,A] vs [A] should be equal
	if !equalCertificateSet([]*siderox509.PEMEncodedCertificate{a, a}, []*siderox509.PEMEncodedCertificate{a}) {
		t.Fatal("duplicate should be set semantics")
	}
}

func TestEqualCertificateSet_Malformed(t *testing.T) {
	t.Parallel()
	malformed := &siderox509.PEMEncodedCertificate{Crt: []byte("not pem")}
	if equalCertificateSet([]*siderox509.PEMEncodedCertificate{malformed}, []*siderox509.PEMEncodedCertificate{malformed}) {
		t.Fatal("malformed should not be considered equal")
	}
}

func TestEqualStringSet_OrderAndDuplicate(t *testing.T) {
	t.Parallel()
	if !equalStringSet([]string{"b", "a"}, []string{"a", "b"}) {
		t.Fatal("string set order should not matter")
	}
	if !equalStringSet([]string{"a", "a"}, []string{"a"}) {
		t.Fatal("string duplicate should be set semantics")
	}
	if equalStringSet([]string{"a"}, []string{"b"}) {
		t.Fatal("different strings should not be equal")
	}
}

func TestSameCertificateAndKeyDER_InvalidMaterial(t *testing.T) {
	t.Parallel()
	validCert := generateTestCertificate(t, "ca")
	validKey := generateTestPrivateKey(t)
	invalid := []byte("invalid")
	left := &siderox509.PEMEncodedCertificateAndKey{Crt: validCert, Key: validKey}
	right := &siderox509.PEMEncodedCertificateAndKey{Crt: invalid, Key: validKey}
	if sameCertificateAndKeyDER(left, right) {
		t.Fatal("invalid PEM should not be considered same")
	}
}
