package artifact

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
)

const SignatureAlgorithmEd25519 = "Ed25519"

type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyID"`
	Value     string `json:"value"`
}

type TrustStore interface {
	Ed25519PublicKey(keyID string) (ed25519.PublicKey, bool)
}

type StaticTrustStore map[string]ed25519.PublicKey

func (store StaticTrustStore) Ed25519PublicKey(keyID string) (ed25519.PublicKey, bool) {
	key, ok := store[keyID]
	return key, ok
}

func VerifySignature(manifest ValidatedManifest, signature Signature, trustStore TrustStore) error {
	if signature.Algorithm != SignatureAlgorithmEd25519 {
		return fmt.Errorf("unsupported signature algorithm: %q", signature.Algorithm)
	}
	if signature.KeyID == "" {
		return errors.New("signature keyID is required")
	}

	publicKey, ok := trustStore.Ed25519PublicKey(signature.KeyID)
	if !ok {
		return errors.New("manifest signature key is not trusted")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("trusted Ed25519 public key has an invalid size")
	}

	value, err := base64.RawStdEncoding.DecodeString(signature.Value)
	if err != nil {
		return errors.New("manifest signature is not valid base64")
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, canonical, value) {
		return errors.New("manifest signature verification failed")
	}
	return nil
}

func Sign(manifest ValidatedManifest, keyID string, privateKey ed25519.PrivateKey) (Signature, error) {
	if keyID == "" {
		return Signature{}, errors.New("signature keyID is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return Signature{}, errors.New("Ed25519 private key has an invalid size")
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return Signature{}, err
	}

	value := ed25519.Sign(privateKey, canonical)
	return Signature{
		Algorithm: SignatureAlgorithmEd25519,
		KeyID:     keyID,
		Value:     base64.RawStdEncoding.EncodeToString(value),
	}, nil
}

func VerifyPayload(reader io.Reader, expectedDigest string, expectedSize int64) error {
	if expectedSize <= 0 {
		return errors.New("expected payload size must be greater than zero")
	}
	if err := validateSHA256Digest(expectedDigest); err != nil {
		return fmt.Errorf("expected payload digest: %w", err)
	}

	hash := sha256.New()
	size, err := io.Copy(hash, reader)
	if err != nil {
		return fmt.Errorf("read payload: %w", err)
	}
	if size != expectedSize {
		return fmt.Errorf("payload size mismatch: got %d, want %d", size, expectedSize)
	}

	actual := digest.NewDigest(digest.SHA256, hash)
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expectedDigest)) != 1 {
		return fmt.Errorf("payload digest mismatch: got %s", actual)
	}
	return nil
}
