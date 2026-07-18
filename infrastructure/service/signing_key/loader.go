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

package signingkey

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// LoadPublicは単一PEM blockからEd25519公開鍵を読み込む。
func LoadPublic(path string) (ed25519.PublicKey, error) {
	block, err := loadPEM(path, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Ed25519 public key: %w", err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key must be Ed25519")
	}
	return publicKey, nil
}

// LoadPrivateは単一PKCS#8 PEM blockからEd25519秘密鍵を読み込む。
func LoadPrivate(path string) (ed25519.PrivateKey, error) {
	block, err := loadPEM(path, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Ed25519 private key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("private key must be Ed25519")
	}
	return privateKey, nil
}

// LoadPrivateReadOnlyはwrite bitがない専用mount上の秘密鍵だけを受け入れる。
func LoadPrivateReadOnly(path string) (ed25519.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat Ed25519 key: %w", err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		return nil, errors.New("private key file must be mounted read-only")
	}
	return LoadPrivate(path)
}

func loadPEM(path, blockType string) (*pem.Block, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Ed25519 key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("key file must contain exactly one PEM block")
	}
	if block.Type != blockType {
		return nil, fmt.Errorf("key PEM block type must be %q", blockType)
	}
	return block, nil
}
