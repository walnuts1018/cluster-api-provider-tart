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
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	artifactoci "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/artifact_oci"
	ociremote "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/oci_remote"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type options struct {
	repository string
	tag        string
	username   string
	password   string
	input      artifactoci.Input
}

func main() {
	opts := parseFlags()
	reference, err := push(context.Background(), opts)
	if err != nil {
		slog.Error("failed to publish OS artifact", "error", err)
		os.Exit(1)
	}
	if _, err := fmt.Fprintln(os.Stdout, reference); err != nil {
		slog.Error("failed to write OS artifact reference", "error", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.repository, "repository", "", "OCI repository without a tag or digest")
	flag.StringVar(&opts.tag, "tag", "", "Immutable build tag to publish")
	flag.StringVar(&opts.username, "username", os.Getenv("GITHUB_ACTOR"), "Registry username")
	flag.StringVar(&opts.password, "password", os.Getenv("GITHUB_TOKEN"), "Registry password or token")
	flag.StringVar(&opts.input.Manifest, "manifest", "", "Path to manifest.json")
	flag.StringVar(&opts.input.Signature, "signature", "", "Path to manifest.signature.json")
	flag.StringVar(&opts.input.Image, "image", "", "Path to OS filesystem payload")
	flag.StringVar(&opts.input.Verity, "verity", "", "Path to dm-verity payload")
	flag.StringVar(&opts.input.Kernel, "kernel", "", "Path to kernel payload")
	flag.StringVar(&opts.input.Initrd, "initrd", "", "Path to initrd payload")
	flag.StringVar(&opts.input.SBOM, "sbom", "", "Path to CycloneDX SBOM")
	flag.StringVar(&opts.input.Provenance, "provenance", "", "Path to in-toto provenance")
	flag.Parse()
	return opts
}

func push(ctx context.Context, opts options) (reference string, err error) {
	if err := validateOptions(opts); err != nil {
		return "", err
	}
	opts.input.Reference = opts.tag

	store, err := file.New(".")
	if err != nil {
		return "", fmt.Errorf("create local OCI store: %w", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			if err == nil {
				err = fmt.Errorf("close local OCI store: %w", closeErr)
			} else {
				slog.Warn("failed to close local OCI store", "error", closeErr)
			}
		}
	}()

	packed, err := artifactoci.Pack(ctx, store, opts.input)
	if err != nil {
		return "", err
	}
	repository, err := remote.NewRepository(opts.repository)
	if err != nil {
		return "", fmt.Errorf("create remote repository: %w", err)
	}
	ociremote.AllowLoopbackPlainHTTP(repository)
	if !ociremote.IsLoopbackRegistry(repository.Reference.Registry) {
		repository.Client = &auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
			Credential: auth.StaticCredential(repository.Reference.Registry, auth.Credential{
				Username: opts.username,
				Password: opts.password,
			}),
		}
	}

	published, err := oras.Copy(ctx, store, opts.tag, repository, opts.tag, oras.DefaultCopyOptions)
	if err != nil {
		return "", fmt.Errorf("publish OCI artifact: %w", err)
	}
	if published.Digest != packed.Digest {
		return "", errors.New("published OCI manifest digest differs from the packed manifest")
	}
	return fmt.Sprintf("oci://%s@%s", opts.repository, published.Digest), nil
}

func validateOptions(opts options) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "repository", value: opts.repository},
		{name: "tag", value: opts.tag},
		{name: "manifest", value: opts.input.Manifest},
		{name: "signature", value: opts.input.Signature},
		{name: "image", value: opts.input.Image},
		{name: "verity", value: opts.input.Verity},
		{name: "kernel", value: opts.input.Kernel},
		{name: "initrd", value: opts.input.Initrd},
		{name: "sbom", value: opts.input.SBOM},
		{name: "provenance", value: opts.input.Provenance},
	}
	for _, item := range required {
		if item.value == "" {
			return fmt.Errorf("-%s is required", item.name)
		}
	}
	return nil
}
