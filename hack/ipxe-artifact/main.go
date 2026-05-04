package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

func main() {
	var downloadDest string
	var tag string
	var repository string
	var username string
	var password string

	flag.StringVar(&downloadDest, "dir", "", "Directory containing files to push")
	flag.StringVar(&tag, "tag", "latest", "Tag for the artifact")
	flag.StringVar(&repository, "repo", "", "Repository to push to (e.g. ghcr.io/user/repo)")
	flag.StringVar(&username, "username", os.Getenv("GITHUB_ACTOR"), "Username for registry")
	flag.StringVar(&password, "password", os.Getenv("GITHUB_TOKEN"), "Password/Token for registry")
	flag.Parse()

	if downloadDest == "" || repository == "" {
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()

	// Parse hostname from repository
	parts := strings.SplitN(repository, "/", 2)
	host := parts[0]

	cred := RepositoryCredential{
		HostName: host,
		Username: username,
		Password: password,
	}

	if err := pushOCIArtifact(ctx, downloadDest, tag, repository, cred); err != nil {
		slog.Error("failed to push OCI artifact", "error", err)
		os.Exit(1)
	}

	slog.Info("successfully pushed OCI artifact", "repository", repository, "tag", tag)
}

type RepositoryCredential struct {
	HostName string
	Username string
	Password string
}

func pushOCIArtifact(ctx context.Context, downloadDest string, tag string, repository string, cred ...RepositoryCredential) error {
	fs, err := file.New(downloadDest)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}
	defer func() {
		if err := fs.Close(); err != nil {
			slog.Warn("failed to close file store", "error", err)
		}
	}()

	files, err := os.ReadDir(downloadDest)
	if err != nil {
		return fmt.Errorf("failed to read files in download destination: %w", err)
	}

	fileDescriptors := make([]ocispec.Descriptor, 0, len(files))
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		mediaType := ocispec.MediaTypeImageLayer

		fileDescriptor, err := fs.Add(ctx, name, mediaType, "")
		if err != nil {
			return fmt.Errorf("failed to add file %s to file store: %w", name, err)
		}
		fileDescriptors = append(fileDescriptors, fileDescriptor)
	}

	repo, err := remote.NewRepository(repository)
	if err != nil {
		return fmt.Errorf("failed to create remote repository: %w", err)
	}

	var credential auth.CredentialFunc
	if len(cred) > 0 {
		credential = auth.StaticCredential(cred[0].HostName, auth.Credential{
			Username: cred[0].Username,
			Password: cred[0].Password,
		})
	}

	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: credential,
	}

	// Create manifests for each architecture
	architectures := []string{"amd64", "arm64"}
	manifests := make([]ocispec.Descriptor, 0, len(architectures))

	for _, arch := range architectures {
		configData, err := json.Marshal(struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		}{
			Architecture: arch,
			OS:           "linux",
		})
		if err != nil {
			return fmt.Errorf("failed to marshal config for %s: %w", arch, err)
		}

		config := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configData)
		if err := repo.Push(ctx, config, bytes.NewReader(configData)); err != nil {
			return fmt.Errorf("failed to push config for %s: %w", arch, err)
		}

		opts := oras.PackManifestOptions{
			Layers:           fileDescriptors,
			ConfigDescriptor: &config,
		}
		manifestDescriptor, err := oras.PackManifest(ctx, fs, oras.PackManifestVersion1_1, ocispec.MediaTypeImageManifest, opts)
		if err != nil {
			return fmt.Errorf("failed to pack manifest for %s: %w", arch, err)
		}

		// Add platform information to the manifest descriptor for the index
		manifestDescriptor.Platform = &ocispec.Platform{
			Architecture: arch,
			OS:           "linux",
		}
		manifests = append(manifests, manifestDescriptor)
	}

	// Create the manifest index
	index := ocispec.Index{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	}

	// Marshal the index to JSON
	indexBytes, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest index: %w", err)
	}

	// Push the index to the file store
	indexDescriptor, err := oras.PushBytes(ctx, fs, ocispec.MediaTypeImageIndex, indexBytes)
	if err != nil {
		return fmt.Errorf("failed to pack manifest index: %w", err)
	}

	if err = fs.Tag(ctx, indexDescriptor, tag); err != nil {
		return fmt.Errorf("failed to tag manifest index: %w", err)
	}

	if _, err = oras.Copy(ctx, fs, tag, repo, tag, oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("failed to push OCI artifact index: %w", err)
	}

	return nil
}
