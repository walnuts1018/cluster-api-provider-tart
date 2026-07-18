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
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type lockFile struct {
	SchemaVersion   int          `json:"schemaVersion"`
	SourceDateEpoch int64        `json:"sourceDateEpoch"`
	Snapshot        string       `json:"snapshot"`
	Mkosi           mkosiLock    `json:"mkosi"`
	Files           []lockedFile `json:"files"`
}

type mkosiLock struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type lockedFile struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

func main() {
	var lockPath string
	var outputDir string
	flag.StringVar(&lockPath, "lock", "", "Path to artifact input lock file")
	flag.StringVar(&outputDir, "output-dir", "", "Directory for verified downloads")
	flag.Parse()

	if err := run(context.Background(), lockPath, outputDir, &http.Client{Timeout: 10 * time.Minute}); err != nil {
		slog.Error("locked download failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, lockPath, outputDir string, client *http.Client) error {
	if lockPath == "" {
		return errors.New("-lock is required")
	}
	if outputDir == "" {
		return errors.New("-output-dir is required")
	}
	lock, err := readLock(lockPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	for _, item := range lock.Files {
		if err := download(ctx, client, outputDir, item); err != nil {
			return fmt.Errorf("download %s: %w", item.Name, err)
		}
	}
	return nil
}

func readLock(path string) (lockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockFile{}, fmt.Errorf("read lock file: %w", err)
	}
	if len(data) > 1<<20 {
		return lockFile{}, errors.New("lock file exceeds 1 MiB")
	}

	var lock lockFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lock); err != nil {
		return lockFile{}, fmt.Errorf("decode lock file: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return lockFile{}, errors.New("lock file must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return lockFile{}, fmt.Errorf("decode trailing lock data: %w", err)
	}
	if lock.SchemaVersion != 1 {
		return lockFile{}, fmt.Errorf("unsupported lock schemaVersion: %d", lock.SchemaVersion)
	}
	if len(lock.Files) == 0 {
		return lockFile{}, errors.New("lock file contains no files")
	}
	names := make(map[string]struct{}, len(lock.Files))
	for _, item := range lock.Files {
		if err := validateLockedFile(item); err != nil {
			return lockFile{}, fmt.Errorf("invalid locked file %q: %w", item.Name, err)
		}
		if _, exists := names[item.Name]; exists {
			return lockFile{}, fmt.Errorf("locked file name is duplicated: %q", item.Name)
		}
		names[item.Name] = struct{}{}
	}
	return lock, nil
}

func validateLockedFile(item lockedFile) error {
	if item.Name == "" || filepath.Base(item.Name) != item.Name {
		return errors.New("name must be a base filename")
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("url must be an absolute HTTPS URL")
	}
	if item.SizeBytes <= 0 || item.SizeBytes > 1<<30 {
		return errors.New("sizeBytes must be between 1 byte and 1 GiB")
	}
	if !sha256Pattern.MatchString(item.SHA256) {
		return errors.New("sha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func download(ctx context.Context, client *http.Client, outputDir string, item lockedFile) error {
	destination := filepath.Join(outputDir, item.Name)
	if err := verifyExisting(destination, item); err == nil {
		slog.Info("locked file already verified", "file", item.Name)
		return nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request file: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			slog.Warn("failed to close download response", "file", item.Name, "error", closeErr)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", response.Status)
	}

	temp, err := os.CreateTemp(outputDir, "."+item.Name+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			slog.Warn("failed to remove temporary download", "file", item.Name, "error", removeErr)
		}
	}()

	hash := sha256.New()
	// lock値より1 byteだけ多く読み、巨大responseを保存する前にsize超過を検出する。
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(response.Body, item.SizeBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return fmt.Errorf("write temporary file: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close temporary file: %w", closeErr)
	}
	if written != item.SizeBytes {
		return fmt.Errorf("size mismatch: got %d, want %d", written, item.SizeBytes)
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actualDigest), []byte(item.SHA256)) != 1 {
		return errors.New("SHA-256 mismatch")
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	slog.Info("locked file downloaded", "file", item.Name)
	return nil
}

func verifyExisting(path string, item lockedFile) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, io.LimitReader(file, item.SizeBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if size != item.SizeBytes {
		return errors.New("size mismatch")
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actualDigest), []byte(item.SHA256)) != 1 {
		return errors.New("SHA-256 mismatch")
	}
	return nil
}
