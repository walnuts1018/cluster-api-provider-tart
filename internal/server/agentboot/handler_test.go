package agentboot

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentartifact"
)

type staticResolver struct {
	target Target
	err    error
}

func (resolver staticResolver) Resolve(context.Context, string) (Target, error) {
	return resolver.target, resolver.err
}

func TestLoadArtifactは署名とPayload検証後だけArtifactを返す(t *testing.T) {
	files := writeArtifactFiles(t)
	artifact, err := LoadArtifact(files)
	if err != nil {
		t.Fatalf("LoadArtifact() error = %v", err)
	}
	if err := artifact.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := os.WriteFile(files.InitrdPath, []byte("changed-initrd"), 0o600); err != nil {
		t.Fatalf("WriteFile(initrd) error = %v", err)
	}
	if _, err := LoadArtifact(files); err == nil {
		t.Fatal("LoadArtifact() accepted modified initrd")
	}
}

func TestHandlerはScriptと固定DigestArtifactを配信する(t *testing.T) {
	files := writeArtifactFiles(t)
	artifact, err := LoadArtifact(files)
	if err != nil {
		t.Fatalf("LoadArtifact() error = %v", err)
	}
	t.Cleanup(func() {
		if err := artifact.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	replacement := files.KernelPath + ".replacement"
	if err := os.WriteFile(replacement, []byte("replaced-after-verification"), 0o600); err != nil {
		t.Fatalf("WriteFile(replacement kernel) error = %v", err)
	}
	if err := os.Rename(replacement, files.KernelPath); err != nil {
		t.Fatalf("Rename(kernel) error = %v", err)
	}
	handler, err := NewHandler(Config{
		Resolver: staticResolver{target: Target{
			HostUID:         "host-uid",
			OperationUID:    "operation-uid",
			BootMACAddress:  "00:00:5e:00:53:01",
			PlatformProfile: agentartifact.PlatformProfileAMD64UEFIABV1,
		}},
		Artifact:        artifact,
		ArtifactBaseURL: "https://boot.test",
		AgentAPIURL:     "https://agent-api.test:8443",
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	scriptRequest := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:01", nil)
	scriptResponse := httptest.NewRecorder()
	handler.ServeHTTP(scriptResponse, scriptRequest)
	if scriptResponse.Code != http.StatusOK {
		t.Fatalf("script status = %d, body = %s", scriptResponse.Code, scriptResponse.Body.String())
	}
	if !strings.Contains(scriptResponse.Body.String(), "tart.agent.operation-uid=operation-uid") {
		t.Fatalf("script does not contain Operation UID:\n%s", scriptResponse.Body.String())
	}

	digestHex := strings.TrimPrefix(artifact.Manifest().Reference, "oci://registry.test/agent@sha256:")
	kernelRequest := httptest.NewRequest(http.MethodGet, "/v1/agent-artifacts/sha256/"+digestHex+"/kernel", nil)
	kernelResponse := httptest.NewRecorder()
	handler.ServeHTTP(kernelResponse, kernelRequest)
	if kernelResponse.Code != http.StatusOK || kernelResponse.Body.String() != "agent-kernel" {
		t.Fatalf("kernel response = %d %q", kernelResponse.Code, kernelResponse.Body.String())
	}
	if got := kernelResponse.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}

	missingRequest := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-artifacts/sha256/"+strings.Repeat("b", 64)+"/initrd",
		nil,
	)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown digest status = %d, want %d", missingResponse.Code, http.StatusNotFound)
	}
}

func TestHandlerは対象外Hostへ終了Scriptを返す(t *testing.T) {
	artifact, err := LoadArtifact(writeArtifactFiles(t))
	if err != nil {
		t.Fatalf("LoadArtifact() error = %v", err)
	}
	t.Cleanup(func() {
		if err := artifact.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	handler, err := NewHandler(Config{
		Resolver:        staticResolver{err: ErrTargetNotFound},
		Artifact:        artifact,
		ArtifactBaseURL: "https://boot.test",
		AgentAPIURL:     "https://agent-api.test",
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:ff", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "#!ipxe\nexit\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func writeArtifactFiles(t *testing.T) ArtifactFiles {
	t.Helper()
	root := t.TempDir()
	kernel := []byte("agent-kernel")
	initrd := []byte("agent-initrd")
	manifest, err := agentartifact.Validate(agentartifact.Manifest{
		SchemaVersion:   agentartifact.SchemaVersion,
		MediaType:       agentartifact.MediaType,
		Reference:       "oci://registry.test/agent@sha256:" + strings.Repeat("a", 64),
		Architecture:    agentartifact.ArchitectureAMD64,
		Firmware:        agentartifact.FirmwareUEFI,
		PlatformProfile: agentartifact.PlatformProfileAMD64UEFIABV1,
		Kernel:          agentartifact.DescriptorFromBytes(kernel),
		Initrd:          agentartifact.DescriptorFromBytes(initrd),
	})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature, err := agentartifact.Sign(manifest, "agent-release", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	manifestData, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	signatureData, err := json.Marshal(signature)
	if err != nil {
		t.Fatalf("Marshal(signature) error = %v", err)
	}
	publicKeyData, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	paths := ArtifactFiles{
		ManifestPath:  filepath.Join(root, "manifest.json"),
		SignaturePath: filepath.Join(root, "manifest.signature.json"),
		KernelPath:    filepath.Join(root, "vmlinuz"),
		InitrdPath:    filepath.Join(root, "initrd"),
		KeyID:         "agent-release",
		PublicKeyPath: filepath.Join(root, "public-key.pem"),
	}
	for path, data := range map[string][]byte{
		paths.ManifestPath:  manifestData,
		paths.SignaturePath: signatureData,
		paths.KernelPath:    kernel,
		paths.InitrdPath:    initrd,
		paths.PublicKeyPath: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyData}),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
	}
	return paths
}
