package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRunDownloadsAndVerifiesLockedFile(t *testing.T) {
	t.Parallel()

	payload := []byte("fixed package")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(payload), nil
	})}

	root := t.TempDir()
	lockPath := writeLock(t, root, lockedFile{
		Name:      "package.deb",
		URL:       "https://packages.sample.walnuts.dev/package.deb",
		SizeBytes: int64(len(payload)),
		SHA256:    digest(payload),
	})
	outputDir := filepath.Join(root, "packages")

	if err := run(context.Background(), lockPath, outputDir, client); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(outputDir, "package.deb"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded payload = %q, want %q", got, payload)
	}

	if err := run(context.Background(), lockPath, outputDir, client); err != nil {
		t.Fatalf("run() with existing file error = %v", err)
	}
}

func TestRunRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	payload := []byte("changed package")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(payload), nil
	})}

	root := t.TempDir()
	lockPath := writeLock(t, root, lockedFile{
		Name:      "package.deb",
		URL:       "https://packages.sample.walnuts.dev/package.deb",
		SizeBytes: int64(len(payload)),
		SHA256:    digest([]byte("original package")),
	})
	outputDir := filepath.Join(root, "packages")

	if err := run(context.Background(), lockPath, outputDir, client); err == nil {
		t.Fatal("run() error = nil, want digest mismatch")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "package.deb")); !os.IsNotExist(err) {
		t.Fatalf("destination exists after verification failure: %v", err)
	}
}

func writeLock(t *testing.T, dir string, item lockedFile) string {
	t.Helper()

	data, err := json.Marshal(lockFile{SchemaVersion: 1, Files: []lockedFile{item}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	path := filepath.Join(dir, "lock.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}

func digest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(payload []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(payload)),
		Header:     make(http.Header),
	}
}
