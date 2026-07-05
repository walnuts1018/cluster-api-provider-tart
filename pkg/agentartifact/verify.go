package agentartifact

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
)

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

func Sign(manifest ValidatedManifest, keyID string, privateKey ed25519.PrivateKey) (Signature, error) {
	if keyID == "" {
		return Signature{}, errors.New("agent Artifact signature keyID is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return Signature{}, errors.New("agent Artifact Ed25519 private key has an invalid size")
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return Signature{}, err
	}
	return Signature{
		Algorithm: SignatureAlgorithmEd25519,
		KeyID:     keyID,
		Value:     base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)),
	}, nil
}

func VerifySignature(manifest ValidatedManifest, signature Signature, trustStore TrustStore) error {
	if trustStore == nil {
		return errors.New("agent Artifact trust store is required")
	}
	if signature.Algorithm != SignatureAlgorithmEd25519 {
		return fmt.Errorf("unsupported Agent Artifact signature algorithm: %q", signature.Algorithm)
	}
	if signature.KeyID == "" {
		return errors.New("agent Artifact signature keyID is required")
	}
	publicKey, ok := trustStore.Ed25519PublicKey(signature.KeyID)
	if !ok {
		return errors.New("agent Artifact signature key is not trusted")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("trusted Agent Artifact Ed25519 public key has an invalid size")
	}
	value, err := base64.RawStdEncoding.DecodeString(signature.Value)
	if err != nil {
		return errors.New("agent Artifact signature is not valid base64")
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, canonical, value) {
		return errors.New("agent Artifact signature verification failed")
	}
	return nil
}
