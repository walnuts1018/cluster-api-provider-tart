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

package redfish

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func TestAdapterContractSessionAuthenticationPreferred(t *testing.T) {
	t.Parallel()

	simulator := startExternalContractSimulator(t, "supported")
	adapter := New()

	if _, err := adapter.DiscoverCapabilities(t.Context(), driverdomain.Redfish, simulator.target, applicationdriver.Invocation{}); err != nil {
		t.Fatalf("DiscoverCapabilities() error = %v", err)
	}

	state := simulator.debugState(t)
	if state.SessionAttempts != 1 {
		t.Fatalf("SessionAttempts = %d, want 1", state.SessionAttempts)
	}
	if state.SessionCreated != 1 {
		t.Fatalf("SessionCreated = %d, want 1", state.SessionCreated)
	}
	if state.BasicAuthRequests != 0 {
		t.Fatalf("BasicAuthRequests = %d, want 0", state.BasicAuthRequests)
	}
	if state.TokenAuthRequests == 0 {
		t.Fatal("TokenAuthRequests = 0, want > 0")
	}
}

func TestAdapterContractFallsBackToBasicOnlyWhenSessionUnsupported(t *testing.T) {
	t.Parallel()

	simulator := startExternalContractSimulator(t, "unsupported")
	adapter := New()

	if _, err := adapter.DiscoverCapabilities(t.Context(), driverdomain.Redfish, simulator.target, applicationdriver.Invocation{}); err != nil {
		t.Fatalf("DiscoverCapabilities() error = %v", err)
	}

	state := simulator.debugState(t)
	if state.SessionAttempts != 0 {
		t.Fatalf("SessionAttempts = %d, want 0", state.SessionAttempts)
	}
	if state.BasicAuthRequests == 0 {
		t.Fatal("BasicAuthRequests = 0, want > 0")
	}
	if state.TokenAuthRequests != 0 {
		t.Fatalf("TokenAuthRequests = %d, want 0", state.TokenAuthRequests)
	}
}

func TestAdapterContractSetsOneTimeBootOverrideAndObservesBootState(t *testing.T) {
	t.Parallel()

	simulator := startExternalContractSimulator(t, "supported")
	adapter := New()
	operationID := mustParseOperationID(t, "f4353748-c9ea-41c6-b321-94197b64330e")
	artifact := mustArtifact(t, "https://controller.example.test/agent.iso")

	if err := adapter.Mount(t.Context(), simulator.target, artifact, operationID); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if err := adapter.SetNextBoot(t.Context(), simulator.target, driverdomain.BootTargetVirtualMedia, operationID); err != nil {
		t.Fatalf("SetNextBoot() error = %v", err)
	}

	state, err := adapter.ObserveBootState(t.Context(), simulator.target)
	if err != nil {
		t.Fatalf("ObserveBootState() error = %v", err)
	}
	if !state.OverrideEnabled {
		t.Fatal("OverrideEnabled = false, want true")
	}
	if state.OverrideTarget != driverdomain.BootTargetVirtualMedia {
		t.Fatalf("OverrideTarget = %q, want VirtualMedia", state.OverrideTarget)
	}
	if !state.MediaInserted {
		t.Fatal("MediaInserted = false, want true")
	}
	if state.MediaImage != artifact.Reference() {
		t.Fatalf("MediaImage = %q, want %q", state.MediaImage, artifact.Reference())
	}
	if state.MediaOperation != operationID.String() {
		t.Fatalf("MediaOperation = %q, want %q", state.MediaOperation, operationID.String())
	}

	debug := simulator.debugState(t)
	if debug.BootPatchCount != 1 {
		t.Fatalf("BootPatchCount = %d, want 1", debug.BootPatchCount)
	}
	if debug.BootEnabled != "Once" {
		t.Fatalf("BootEnabled = %q, want Once", debug.BootEnabled)
	}
	if debug.BootTarget != "Cd" {
		t.Fatalf("BootTarget = %q, want Cd", debug.BootTarget)
	}
}

func TestAdapterContractVirtualMediaIdempotencyAndConflict(t *testing.T) {
	t.Parallel()

	simulator := startExternalContractSimulator(t, "supported")
	adapter := New()
	operationID := mustParseOperationID(t, "f4353748-c9ea-41c6-b321-94197b64330e")
	conflictingOperationID := mustParseOperationID(t, "11111111-1111-1111-1111-111111111111")
	artifact := mustArtifact(t, "https://controller.example.test/agent.iso")
	conflictingArtifact := mustArtifact(t, "https://controller.example.test/other.iso")

	if err := adapter.Mount(t.Context(), simulator.target, artifact, operationID); err != nil {
		t.Fatalf("Mount() first call error = %v", err)
	}
	if err := adapter.Mount(t.Context(), simulator.target, artifact, operationID); err != nil {
		t.Fatalf("Mount() second call error = %v", err)
	}
	if err := adapter.Mount(t.Context(), simulator.target, conflictingArtifact, conflictingOperationID); !driverdomain.IsErrorKind(err, driverdomain.ErrorConflict) {
		t.Fatalf("Mount() conflicting call error = %v, want Conflict", err)
	}

	state := simulator.debugState(t)
	if state.InsertCount != 1 {
		t.Fatalf("InsertCount = %d, want 1", state.InsertCount)
	}
	if !state.MediaInserted {
		t.Fatal("MediaInserted = false, want true")
	}
	if state.MediaImage != artifact.Reference() {
		t.Fatalf("MediaImage = %q, want %q", state.MediaImage, artifact.Reference())
	}
	if state.MediaOperationID != operationID.String() {
		t.Fatalf("MediaOperationID = %q, want %q", state.MediaOperationID, operationID.String())
	}
}

type externalContractSimulator struct {
	baseURL  string
	caBundle []byte
	target   driverdomain.HostTarget
}

type externalContractDebugState struct {
	SessionAttempts   int    `json:"sessionAttempts"`
	SessionCreated    int    `json:"sessionCreated"`
	BasicAuthRequests int    `json:"basicAuthRequests"`
	TokenAuthRequests int    `json:"tokenAuthRequests"`
	BootPatchCount    int    `json:"bootPatchCount"`
	InsertCount       int    `json:"insertCount"`
	BootEnabled       string `json:"bootEnabled"`
	BootTarget        string `json:"bootTarget"`
	MediaInserted     bool   `json:"mediaInserted"`
	MediaImage        string `json:"mediaImage"`
	MediaOperationID  string `json:"mediaOperationID"`
}

type externalContractReadyPayload struct {
	Endpoint    string `json:"endpoint"`
	CABundlePEM string `json:"caBundlePEM"`
}

var (
	externalContractBuildOnce sync.Once
	externalContractBuildPath string
	externalContractBuildErr  error
)

func startExternalContractSimulator(t *testing.T, sessionMode string) externalContractSimulator {
	t.Helper()

	binaryPath := externalContractSimulatorBinary(t)
	readyFile := filepath.Join(t.TempDir(), "ready.json")
	ctx, cancel := context.WithCancel(t.Context())

	command := exec.CommandContext(
		ctx,
		binaryPath,
		"-session-mode", sessionMode,
		"-ready-file", readyFile,
	)
	command.Dir = externalContractRepoRoot(t)
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(externalContractRepoRoot(t), ".cache", "go-build"))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		cancel()
		waitErr := command.Wait()
		if waitErr != nil && ctx.Err() == nil {
			t.Fatalf("Wait() error = %v", waitErr)
		}
	})

	ready := waitForExternalContractReadyFile(t, readyFile, &stderr)
	target := externalContractTarget(t, ready.Endpoint, []byte(ready.CABundlePEM))
	return externalContractSimulator{
		baseURL:  ready.Endpoint,
		caBundle: []byte(ready.CABundlePEM),
		target:   target,
	}
}

func (simulator externalContractSimulator) debugState(t *testing.T) externalContractDebugState {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, simulator.baseURL+"/debug/state", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := (&http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: externalContractCertPool(t, simulator.caBundle)}},
	}).Do(request)
	if err != nil {
		t.Fatalf("GET /debug/state error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /debug/state status = %d, want 200", response.StatusCode)
	}

	var state externalContractDebugState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return state
}

func externalContractCertPool(t *testing.T, bundle []byte) *x509.CertPool {
	t.Helper()

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		t.Fatal("AppendCertsFromPEM() = false, want true")
	}
	return pool
}

func externalContractSimulatorBinary(t *testing.T) string {
	t.Helper()

	externalContractBuildOnce.Do(func() {
		binaryDir := t.TempDir()
		externalContractBuildPath = filepath.Join(binaryDir, "redfish-simulator")
		command := exec.CommandContext(t.Context(), "go", "build", "-o", externalContractBuildPath, "./hack/redfish-simulator")
		command.Dir = externalContractRepoRoot(t)
		command.Env = append(
			os.Environ(),
			"GOCACHE="+filepath.Join(externalContractRepoRoot(t), ".cache", "go-build"),
			"GOMODCACHE="+filepath.Join(externalContractRepoRoot(t), ".cache", "go-mod"),
		)
		output, err := command.CombinedOutput()
		if err != nil {
			externalContractBuildErr = fmt.Errorf("go build ./hack/redfish-simulator: %w\n%s", err, output)
		}
	})
	if externalContractBuildErr != nil {
		t.Fatal(externalContractBuildErr)
	}
	return externalContractBuildPath
}

func externalContractRepoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

func waitForExternalContractReadyFile(t *testing.T, path string, stderrOutput *bytes.Buffer) externalContractReadyPayload {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		payload, err := os.ReadFile(path)
		if err == nil {
			var ready externalContractReadyPayload
			if err := json.Unmarshal(payload, &ready); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", path, err)
			}
			return ready
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("simulator did not become ready: %s", stderrOutput.String())
	return externalContractReadyPayload{}
}

func externalContractTarget(t *testing.T, endpoint string, caBundle []byte) driverdomain.HostTarget {
	t.Helper()

	address, err := driverdomain.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	access, err := driverdomain.NewRedfishAccess(endpoint, "admin", "secret", caBundle, nil)
	if err != nil {
		t.Fatalf("NewRedfishAccess() error = %v", err)
	}
	return driverdomain.NewHostTarget(address).WithRedfishAccess(access)
}

func mustParseOperationID(t *testing.T, value string) operationdomain.ID {
	t.Helper()

	operationID, err := operationdomain.ParseID(value)
	if err != nil {
		t.Fatalf("ParseID(%q) error = %v", value, err)
	}
	return operationID
}

func mustArtifact(t *testing.T, value string) driverdomain.Artifact {
	t.Helper()

	artifact, err := driverdomain.NewArtifact(value)
	if err != nil {
		t.Fatalf("NewArtifact(%q) error = %v", value, err)
	}
	return artifact
}
