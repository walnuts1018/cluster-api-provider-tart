package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

func main() {
	var dir string
	var tag string
	var repository string
	var username string
	var password string

	flag.StringVar(&dir, "dir", "", "Directory containing files to push")
	flag.StringVar(&tag, "tag", "latest", "Tag for the artifact")
	flag.StringVar(&repository, "repo", "", "Repository to push to (e.g. ghcr.io/user/repo)")
	flag.StringVar(&username, "username", os.Getenv("GITHUB_ACTOR"), "Username for registry")
	flag.StringVar(&password, "password", os.Getenv("GITHUB_TOKEN"), "Password/Token for registry")
	flag.Parse()

	if dir == "" || repository == "" {
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()

	parts := strings.SplitN(repository, "/", 2)
	host := parts[0]

	cred := RepositoryCredential{
		HostName: host,
		Username: username,
		Password: password,
	}

	if err := pushOCIArtifact(ctx, dir, tag, repository, cred); err != nil {
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

func pushOCIArtifact(ctx context.Context, dir string, tag string, repository string, cred ...RepositoryCredential) error {
	store := memory.New()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read dir: %w", err)
	}

	var layerDescs []ocispecv1.Descriptor

	for _, entry := range entries {
		tarGzBytes, err := buildTarGzForEntry(dir, entry.Name())
		if err != nil {
			return fmt.Errorf("failed to build tar.gz for %s: %w", entry.Name(), err)
		}

		desc := content.NewDescriptorFromBytes(ocispecv1.MediaTypeImageLayerGzip, tarGzBytes)
		if err := store.Push(ctx, desc, bytes.NewReader(tarGzBytes)); err != nil {
			return fmt.Errorf("failed to push layer for %s: %w", entry.Name(), err)
		}
		layerDescs = append(layerDescs, desc)
	}

	arches := []string{"amd64", "arm64"}
	var manifestDescs []ocispecv1.Descriptor

	for _, arch := range arches {
		configData, err := json.Marshal(struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		}{
			Architecture: arch,
			OS:           "linux",
		})
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		configDesc := content.NewDescriptorFromBytes(ocispecv1.MediaTypeImageConfig, configData)
		if err := store.Push(ctx, configDesc, bytes.NewReader(configData)); err != nil {
			return fmt.Errorf("failed to push config for %s: %w", arch, err)
		}

		packOpts := oras.PackManifestOptions{
			Layers:           layerDescs,
			ConfigDescriptor: &configDesc,
		}
		manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, ocispecv1.MediaTypeImageManifest, packOpts)
		if err != nil {
			return fmt.Errorf("failed to pack manifest for %s: %w", arch, err)
		}

		manifestDesc.Platform = &ocispecv1.Platform{
			Architecture: arch,
			OS:           "linux",
		}
		manifestDescs = append(manifestDescs, manifestDesc)
	}

	index := ocispecv1.Index{
		Versioned: ocispec.Versioned{SchemaVersion: 2},
		MediaType: ocispecv1.MediaTypeImageIndex,
		Manifests: manifestDescs,
	}
	indexBytes, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}
	indexDesc := content.NewDescriptorFromBytes(ocispecv1.MediaTypeImageIndex, indexBytes)
	if err := store.Push(ctx, indexDesc, bytes.NewReader(indexBytes)); err != nil {
		return fmt.Errorf("failed to push index: %w", err)
	}

	if err := store.Tag(ctx, indexDesc, tag); err != nil {
		return fmt.Errorf("failed to tag index: %w", err)
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

	if _, err = oras.Copy(ctx, store, tag, repo, tag, oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("failed to push OCI artifact: %w", err)
	}

	return nil
}

func buildTarGzForEntry(baseDir, entryName string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	targetPath := filepath.Join(baseDir, entryName)

	err := filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
