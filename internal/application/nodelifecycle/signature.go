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

package nodelifecycle

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func Sign(plan ValidatedPlan, keyID string, privateKey ed25519.PrivateKey) (agentprotocol.Signature, error) {
	if keyID == "" {
		return agentprotocol.Signature{}, errors.New("signature keyID is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return agentprotocol.Signature{}, errors.New("Ed25519 private key has an invalid size")
	}
	canonical, err := plan.CanonicalJSON()
	if err != nil {
		return agentprotocol.Signature{}, err
	}
	return agentprotocol.Signature{
		Algorithm: agentprotocol.SignatureAlgorithm,
		KeyID:     keyID,
		Value:     base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)),
	}, nil
}

func VerifySignature(plan ValidatedPlan, signature agentprotocol.Signature, trustStore agentprotocol.TrustStore) error {
	if signature.Algorithm != agentprotocol.SignatureAlgorithm {
		return fmt.Errorf("unsupported signature algorithm: %q", signature.Algorithm)
	}
	if signature.KeyID == "" {
		return errors.New("signature keyID is required")
	}
	publicKey, ok := trustStore.Ed25519PublicKey(signature.KeyID)
	if !ok {
		return errors.New("lifecycle plan signature key is not trusted")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("trusted Ed25519 public key has an invalid size")
	}
	value, err := base64.RawStdEncoding.DecodeString(signature.Value)
	if err != nil {
		return errors.New("lifecycle plan signature is not valid base64")
	}
	canonical, err := plan.CanonicalJSON()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, canonical, value) {
		return errors.New("lifecycle plan signature verification failed")
	}
	return nil
}
