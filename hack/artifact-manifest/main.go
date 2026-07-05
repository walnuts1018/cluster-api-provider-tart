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

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

type config struct {
	OS              artifact.OS           `json:"os"`
	Architecture    string                `json:"architecture"`
	Filesystem      string                `json:"filesystem"`
	StateSchema     artifact.StateSchema  `json:"stateSchema"`
	Kubernetes      artifact.Kubernetes   `json:"kubernetes"`
	Requirements    artifact.Requirements `json:"requirements"`
	Generation      uint64                `json:"generation"`
	PlatformProfile string                `json:"platformProfile"`
}

type options struct {
	configPath     string
	imagePath      string
	verityPath     string
	verityRootHash string
	kernelPath     string
	initrdPath     string
	signingKeyPath string
	keyID          string
	outputDir      string
}

func main() {
	if err := run(parseFlags()); err != nil {
		fmt.Fprintf(os.Stderr, "artifact manifest generation failed: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.configPath, "config", "", "Path to immutable manifest metadata")
	flag.StringVar(&opts.imagePath, "image", "", "Path to OS filesystem payload")
	flag.StringVar(&opts.verityPath, "verity", "", "Path to dm-verity payload")
	flag.StringVar(&opts.verityRootHash, "verity-root-hash", "", "dm-verity root hash")
	flag.StringVar(&opts.kernelPath, "kernel", "", "Path to kernel payload")
	flag.StringVar(&opts.initrdPath, "initrd", "", "Path to initrd payload")
	flag.StringVar(&opts.signingKeyPath, "signing-key", "", "Path to an Ed25519 PKCS#8 PEM private key")
	flag.StringVar(&opts.keyID, "key-id", "", "Trust policy key identifier")
	flag.StringVar(&opts.outputDir, "output-dir", "", "Directory for manifest.json and manifest.signature.json")
	flag.Parse()
	return opts
}

func run(opts options) error {
	if err := validateOptions(opts); err != nil {
		return err
	}

	cfg, err := readConfig(opts.configPath)
	if err != nil {
		return err
	}
	image, err := describeFile(opts.imagePath)
	if err != nil {
		return fmt.Errorf("describe image payload: %w", err)
	}
	verityPayload, err := describeFile(opts.verityPath)
	if err != nil {
		return fmt.Errorf("describe verity payload: %w", err)
	}
	kernel, err := describeFile(opts.kernelPath)
	if err != nil {
		return fmt.Errorf("describe kernel payload: %w", err)
	}
	initrd, err := describeFile(opts.initrdPath)
	if err != nil {
		return fmt.Errorf("describe initrd payload: %w", err)
	}

	manifest, err := artifact.Validate(artifact.Manifest{
		SchemaVersion: artifact.SchemaVersion,
		MediaType:     artifact.MediaType,
		OS:            cfg.OS,
		Architecture:  cfg.Architecture,
		Filesystem:    cfg.Filesystem,
		Image:         image,
		Verity: artifact.Verity{
			Digest:    verityPayload.Digest,
			SizeBytes: verityPayload.SizeBytes,
			RootHash:  opts.verityRootHash,
		},
		StateSchema:     cfg.StateSchema,
		Kubernetes:      cfg.Kubernetes,
		Boot:            artifact.Boot{KernelDigest: kernel.Digest, InitrdDigest: initrd.Digest},
		Requirements:    cfg.Requirements,
		Generation:      cfg.Generation,
		PlatformProfile: cfg.PlatformProfile,
	})
	if err != nil {
		return err
	}

	privateKey, err := readPrivateKey(opts.signingKeyPath)
	if err != nil {
		return err
	}
	signature, err := artifact.Sign(manifest, opts.keyID, privateKey)
	if err != nil {
		return err
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	signatureJSON, err := json.Marshal(signature)
	if err != nil {
		return fmt.Errorf("marshal manifest signature: %w", err)
	}

	if err := os.MkdirAll(opts.outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := writeAtomic(filepath.Join(opts.outputDir, "manifest.json"), append(canonical, '\n'), 0o644); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(opts.outputDir, "manifest.signature.json"), append(signatureJSON, '\n'), 0o644); err != nil {
		return err
	}
	return nil
}

func validateOptions(opts options) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "config", value: opts.configPath},
		{name: "image", value: opts.imagePath},
		{name: "verity", value: opts.verityPath},
		{name: "verity-root-hash", value: opts.verityRootHash},
		{name: "kernel", value: opts.kernelPath},
		{name: "initrd", value: opts.initrdPath},
		{name: "signing-key", value: opts.signingKeyPath},
		{name: "key-id", value: opts.keyID},
		{name: "output-dir", value: opts.outputDir},
	}
	for _, item := range required {
		if item.value == "" {
			return fmt.Errorf("-%s is required", item.name)
		}
	}
	return nil
}

func readConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}
	if len(data) > 1<<20 {
		return config{}, errors.New("config exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var cfg config
	if err := decoder.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("decode config: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return config{}, errors.New("config must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return config{}, fmt.Errorf("decode trailing config data: %w", err)
	}
	return cfg, nil
}

func describeFile(path string) (artifact.Payload, error) {
	file, err := os.Open(path)
	if err != nil {
		return artifact.Payload{}, err
	}
	description, describeErr := artifact.DescribePayload(file)
	closeErr := file.Close()
	if describeErr != nil {
		return artifact.Payload{}, describeErr
	}
	if closeErr != nil {
		return artifact.Payload{}, fmt.Errorf("close payload: %w", closeErr)
	}
	return description, nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	block, trailing := pem.Decode(data)
	if block == nil || len(trailing) != 0 || block.Type != "PRIVATE KEY" {
		return nil, errors.New("signing key must contain one PKCS#8 PRIVATE KEY PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("signing key must be an Ed25519 private key")
	}
	return privateKey, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "failed to remove temporary output: %v\n", removeErr)
		}
	}()

	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set output mode: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write output: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync output: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace output: %w", err)
	}
	return nil
}
