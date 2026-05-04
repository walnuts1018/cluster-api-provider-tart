package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	iPXERepoURL    = "https://github.com/ipxe/ipxe.git"
	iPXEBranch     = "master"
	iPXEefiName    = "ipxe.efi"
	iPXEefiRelPath = "bin/ipxe.efi"
	licenseName    = "GPLv2.txt"
	licenseRelPath = "licenses/GPLv2.txt"
)

type RegistryCredential struct {
	HostName string
	Username string
	Password string
}

func main() {
	var (
		iPXEVersion       string
		repository        string
		tag               string
		ghcrToken         string
		downloadDest      string
		buildParallelJobs int
	)

	flag.StringVar(&iPXEVersion, "ipxe-version", "master", "iPXE git branch or tag to build")
	flag.StringVar(&repository, "repository", "", "OCI repository to push (e.g. ghcr.io/org/repo)")
	flag.StringVar(&tag, "tag", "latest", "Tag to assign to the pushed artifact")
	flag.StringVar(&ghcrToken, "ghcr-token", "", "GitHub token for pushing to GHCR (uses GITHUB_TOKEN env if empty)")
	flag.StringVar(&downloadDest, "download-destination", "", "Local directory for downloading and building iPXE")
	flag.IntVar(&buildParallelJobs, "jobs", runtime.NumCPU(), "Number of parallel build jobs")
	flag.Parse()

	if ghcrToken == "" {
		ghcrToken = os.Getenv("GITHUB_TOKEN")
	}
	if ghcrToken == "" {
		slog.Error("ghcr-token flag or GITHUB_TOKEN env is required")
		os.Exit(1)
	}
	if repository == "" {
		slog.Error("-repository flag is required")
		os.Exit(1)
	}
	if downloadDest == "" {
		downloadDest = "/tmp/ipxe-build"
	}

	ctx := context.Background()

	if err := run(ctx, iPXEVersion, repository, tag, ghcrToken, downloadDest, buildParallelJobs); err != nil {
		slog.ErrorContext(ctx, "build and push failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, iPXEVersion, repository, tag, ghcrToken, downloadDest string, jobs int) error {
	slog.InfoContext(ctx, "starting iPXE build and push",
		slog.String("version", iPXEVersion),
		slog.String("repository", repository),
		slog.String("tag", tag),
		slog.String("destination", downloadDest),
	)

	if err := os.MkdirAll(downloadDest, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	// Step 1: Clone iPXE source
	if err := cloneIPXE(ctx, downloadDest, iPXEVersion); err != nil {
		return fmt.Errorf("failed to clone iPXE: %w", err)
	}

	// Step 2: Build ipxe.efi
	ipxeDir := filepath.Join(downloadDest, "ipxe")
	if err := buildIPXE(ctx, ipxeDir, jobs); err != nil {
		return fmt.Errorf("failed to build iPXE: %w", err)
	}

	// Step 3: Push to OCI registry
	cred := RegistryCredential{
		HostName: "ghcr.io",
		Username: "x-access-token",
		Password: ghcrToken,
	}
	if err := pushOCIArtifact(ctx, downloadDest, tag, repository, cred); err != nil {
		return fmt.Errorf("failed to push OCI artifact: %w", err)
	}

	slog.InfoContext(ctx, "successfully built and pushed iPXE artifact",
		slog.String("repository", repository),
		slog.String("tag", tag),
	)

	return nil
}

func cloneIPXE(ctx context.Context, dest, version string) error {
	slog.InfoContext(ctx, "cloning iPXE source", slog.String("version", version))

	ipxeDir := filepath.Join(dest, "ipxe")
	if _, err := os.Stat(ipxeDir); err == nil {
		slog.InfoContext(ctx, "iPXE source already exists, skipping clone")
		return nil
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", version, iPXERepoURL, ipxeDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	slog.InfoContext(ctx, "successfully cloned iPXE source")
	return nil
}

func buildIPXE(ctx context.Context, ipxeDir string, jobs int) error {
	slog.InfoContext(ctx, "building iPXE", slog.Int("jobs", jobs))

	// Check if nasm is available
	if _, err := exec.LookPath("nasm"); err != nil {
		return fmt.Errorf("nasm not found: %w. Please install nasm (e.g. brew install nasm on macOS, apt install nasm on Linux)", err)
	}
	if _, err := exec.LookPath("gcc"); err != nil {
		return fmt.Errorf("gcc not found: %w. Please install gcc", err)
	}

	// Build ipxe.efi
	cmd := exec.CommandContext(ctx, "make", "bin/ipxe.efi", fmt.Sprintf("-j%d", jobs))
	cmd.Dir = ipxeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make failed: %w", err)
	}

	// Verify the binary was built
	efiPath := filepath.Join(ipxeDir, "bin", iPXEefiName)
	if _, err := os.Stat(efiPath); err != nil {
		return fmt.Errorf("built iPXE binary not found at %s: %w", efiPath, err)
	}

	slog.InfoContext(ctx, "successfully built iPXE binary", slog.String("path", efiPath))
	return nil
}

func prepareBuildFiles(dest string) error {
	ipxeDir := filepath.Join(dest, "ipxe")

	// Create output directory
	outputDir := filepath.Join(dest, "artifact")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Copy ipxe.efi
	efiSrc := filepath.Join(ipxeDir, iPXEefiRelPath)
	efiDst := filepath.Join(outputDir, iPXEefiName)
	if err := copyFile(efiSrc, efiDst); err != nil {
		return fmt.Errorf("failed to copy ipxe.efi: %w", err)
	}

	// Copy license
	licenseSrc := filepath.Join(ipxeDir, licenseRelPath)
	licenseDst := filepath.Join(outputDir, licenseName)
	if err := copyFile(licenseSrc, licenseDst); err != nil {
		return fmt.Errorf("failed to copy license: %w", err)
	}

	slog.Info("prepared artifact files",
		slog.String("ipxe.efi", efiDst),
		slog.String("license", licenseDst),
	)

	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Chmod(0644)
}

func pushOCIArtifact(ctx context.Context, buildDest string, tag string, repository string, cred RegistryCredential) error {
	slog.InfoContext(ctx, "pushing OCI artifact",
		slog.String("repository", repository),
		slog.String("tag", tag),
	)

	// Create file store pointing to the build destination
	// The file store will read files from the artifact subdirectory
	fs, err := file.New(buildDest)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}
	defer fs.Close()

	// Read files from the artifact directory
	artifactDir := filepath.Join(buildDest, "artifact")
	files, err := os.ReadDir(artifactDir)
	if err != nil {
		return fmt.Errorf("failed to read artifact directory: %w", err)
	}

	fileDescriptors := make([]ocispec.Descriptor, 0, len(files))
	for _, file := range files {
		name := file.Name()

		fileDescriptor, err := fs.Add(ctx, name, ocispec.MediaTypeImageLayerGzip, "")
		if err != nil {
			return fmt.Errorf("failed to add file %s to file store: %w", name, err)
		}
		fileDescriptors = append(fileDescriptors, fileDescriptor)
	}

	// Create remote repository
	repo, err := remote.NewRepository(repository)
	if err != nil {
		return fmt.Errorf("failed to create remote repository: %w", err)
	}

	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(cred.HostName, auth.Credential{
			Username: cred.Username,
			Password: cred.Password,
		}),
	}

	// Create image config
	configData, err := json.Marshal(struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	}{
		Architecture: "amd64",
		OS:           "linux",
	})
	if err != nil {
		return fmt.Errorf("failed to marshal config to JSON: %w", err)
	}

	config := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configData)
	if err := repo.Push(ctx, config, bytes.NewReader(configData)); err != nil {
		return fmt.Errorf("failed to push config: %w", err)
	}

	// Pack manifest
	opts := oras.PackManifestOptions{
		Layers:           fileDescriptors,
		ConfigDescriptor: &config,
	}
	manifestDescriptor, err := oras.PackManifest(ctx, fs, oras.PackManifestVersion1_1, ocispec.MediaTypeImageManifest, opts)
	if err != nil {
		return fmt.Errorf("failed to pack manifest: %w", err)
	}

	// Tag the manifest
	if err = fs.Tag(ctx, manifestDescriptor, tag); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}

	// Copy to remote repository
	if _, err = oras.Copy(ctx, fs, tag, repo, tag, oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("failed to push OCI artifact: %w", err)
	}

	slog.InfoContext(ctx, "successfully pushed OCI artifact",
		slog.String("repository", repository),
		slog.String("tag", tag),
	)

	return nil
}
