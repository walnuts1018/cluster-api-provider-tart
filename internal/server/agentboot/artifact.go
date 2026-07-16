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

package agentboot

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentartifact"
)

type ArtifactFiles struct {
	ManifestPath     string
	SignaturePath    string
	KernelPath       string
	InitrdPath       string
	VirtualMediaPath string
	KeyID            string
	PublicKeyPath    string
}

type Artifact struct {
	manifest agentartifact.ValidatedManifest
	kernel   *os.File
	initrd   *os.File
	media    *os.File
	digest   digest.Digest
}

func LoadArtifact(files ArtifactFiles) (Artifact, error) {
	if files.KeyID == "" {
		return Artifact{}, errors.New("agent Artifact signing key ID is required")
	}
	manifestData, err := os.ReadFile(files.ManifestPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("read Agent Artifact manifest: %w", err)
	}
	manifest, err := agentartifact.Parse(manifestData)
	if err != nil {
		return Artifact{}, err
	}
	signatureData, err := os.ReadFile(files.SignaturePath)
	if err != nil {
		return Artifact{}, fmt.Errorf("read Agent Artifact signature: %w", err)
	}
	var signature agentartifact.Signature
	if err := decodeStrict(signatureData, &signature); err != nil {
		return Artifact{}, fmt.Errorf("decode Agent Artifact signature: %w", err)
	}
	publicKey, err := loadEd25519PublicKey(files.PublicKeyPath)
	if err != nil {
		return Artifact{}, err
	}
	if err := agentartifact.VerifySignature(
		manifest,
		signature,
		agentartifact.StaticTrustStore{files.KeyID: publicKey},
	); err != nil {
		return Artifact{}, fmt.Errorf("verify Agent Artifact signature: %w", err)
	}
	kernel, err := os.Open(files.KernelPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("open Agent Artifact kernel: %w", err)
	}
	initrd, err := os.Open(files.InitrdPath)
	if err != nil {
		closeErr := kernel.Close()
		return Artifact{}, errors.Join(
			fmt.Errorf("open Agent Artifact initrd: %w", err),
			wrapCloseError("kernel", closeErr),
		)
	}
	verifyErr := agentartifact.VerifyPayloads(manifest, kernel, initrd)
	if verifyErr != nil {
		closeErr := closePayloads(kernel, initrd, nil)
		return Artifact{}, errors.Join(verifyErr, closeErr)
	}
	var media *os.File
	if files.VirtualMediaPath != "" {
		media, err = os.Open(files.VirtualMediaPath)
		if err != nil {
			return Artifact{}, errors.Join(
				fmt.Errorf("open Agent Artifact virtual media: %w", err),
				closePayloads(kernel, initrd, nil),
			)
		}
		if err := agentartifact.VerifyVirtualMediaPayload(manifest, media); err != nil {
			return Artifact{}, errors.Join(err, closePayloads(kernel, initrd, nil), wrapCloseError("virtual media", media.Close()))
		}
	}
	// 署名検証後のpath差し替えを配信へ反映しないため、検証したfile descriptorを保持する。
	if _, err := kernel.Seek(0, io.SeekStart); err != nil {
		return Artifact{}, errors.Join(
			fmt.Errorf("rewind Agent Artifact kernel: %w", err),
			closePayloads(kernel, initrd, media),
		)
	}
	if _, err := initrd.Seek(0, io.SeekStart); err != nil {
		return Artifact{}, errors.Join(
			fmt.Errorf("rewind Agent Artifact initrd: %w", err),
			closePayloads(kernel, initrd, media),
		)
	}
	if media != nil {
		if _, err := media.Seek(0, io.SeekStart); err != nil {
			return Artifact{}, errors.Join(
				fmt.Errorf("rewind Agent Artifact virtual media: %w", err),
				closePayloads(kernel, initrd, media),
			)
		}
	}
	reference := manifest.Value().Reference
	referenceDigest := digest.Digest(reference[strings.LastIndex(reference, "@")+1:])
	if err := referenceDigest.Validate(); err != nil {
		return Artifact{}, errors.Join(
			fmt.Errorf("validate Agent Artifact reference digest: %w", err),
			closePayloads(kernel, initrd, media),
		)
	}
	return Artifact{
		manifest: manifest,
		kernel:   kernel,
		initrd:   initrd,
		media:    media,
		digest:   referenceDigest,
	}, nil
}

func (artifact Artifact) Close() error {
	if artifact.kernel == nil && artifact.initrd == nil && artifact.media == nil {
		return nil
	}
	return closePayloads(artifact.kernel, artifact.initrd, artifact.media)
}

func closePayloads(kernel, initrd, media *os.File) error {
	var kernelErr, initrdErr, mediaErr error
	if kernel != nil {
		kernelErr = wrapCloseError("kernel", kernel.Close())
	}
	if initrd != nil {
		initrdErr = wrapCloseError("initrd", initrd.Close())
	}
	if media != nil {
		mediaErr = wrapCloseError("virtual media", media.Close())
	}
	return errors.Join(kernelErr, initrdErr, mediaErr)
}

func wrapCloseError(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close Agent Artifact %s: %w", name, err)
}

func (artifact Artifact) Manifest() agentartifact.Manifest {
	return artifact.manifest.Value()
}

func (artifact Artifact) payload(name string) (*os.File, int64, error) {
	var file *os.File
	var size int64
	switch name {
	case "kernel":
		file = artifact.kernel
		size = artifact.Manifest().Kernel.SizeBytes
	case "initrd":
		file = artifact.initrd
		size = artifact.Manifest().Initrd.SizeBytes
	case "virtual-media":
		file = artifact.media
		if artifact.Manifest().VirtualMedia != nil {
			size = artifact.Manifest().VirtualMedia.SizeBytes
		}
	default:
		return nil, 0, errors.New("unknown Agent Artifact payload")
	}
	if file == nil {
		return nil, 0, errors.New("agent Artifact payload is closed")
	}
	return file, size, nil
}

func (artifact Artifact) VirtualMediaURL(baseURL string) (string, error) {
	if artifact.media == nil || artifact.Manifest().VirtualMedia == nil {
		return "", errors.New("agent Artifact does not contain a VirtualMedia payload")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse Agent Artifact base URL: %w", err)
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("agent Artifact base URL must be an HTTP(S) origin or base path")
	}
	return url.JoinPath(
		parsed.String(),
		"v1",
		"agent-artifacts",
		"sha256",
		artifact.digest.Encoded(),
		"virtual-media",
	)
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("document must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func loadEd25519PublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Agent Artifact public key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("agent Artifact public key file must contain exactly one PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Agent Artifact public key: %w", err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("agent Artifact public key must be Ed25519")
	}
	return publicKey, nil
}
