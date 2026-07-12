package registrycredential

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReturnsNilWithoutConfigPath(t *testing.T) {
	credential, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if credential != nil {
		t.Fatal("Load() returned credential for empty config path")
	}
}

func TestLoadReadsDockerCompatibleConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "auths": {
    "registry.test.walnuts.dev": {
      "username": "octo",
      "password": "secret"
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	credential, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, err := credential(context.Background(), "registry.test.walnuts.dev")
	if err != nil {
		t.Fatalf("credential() error = %v", err)
	}
	if got.Username != "octo" || got.Password != "secret" {
		t.Fatalf("credential() = %#v", got)
	}
}
