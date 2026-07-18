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

package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"

	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

func TestServiceはBootstrap適用成功後にPayload原本を削除しMarkerだけ残す(t *testing.T) {
	applier := &recordingApplier{}
	stateDir := filepath.Join(t.TempDir(), "state")
	workDir := filepath.Join(t.TempDir(), "work")
	service := newTestService(t, stateDir, workDir, applier)
	bundle := testBundle()

	if err := service.Apply(t.Context(), bundle); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, err := os.Stat(applier.payloadPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("payload original still exists: err=%v", err)
	}
	marker, err := service.readMarker()
	if err != nil {
		t.Fatalf("readMarker() error = %v", err)
	}
	if marker.PayloadDigest != bundle.PayloadDigest || marker.OperationUID != bundle.OperationUID {
		t.Fatalf("marker = %#v, want digest=%q operation=%q", marker, bundle.PayloadDigest, bundle.OperationUID)
	}
	applied, err := service.Applied(bundle.OperationUID)
	if err != nil {
		t.Fatalf("Applied() error = %v", err)
	}
	if !applied {
		t.Fatal("Applied() = false, want true")
	}
}

func TestServiceはAdapter失敗時にPayload原本を残す(t *testing.T) {
	applier := &recordingApplier{err: errors.New("adapter failed")}
	service := newTestService(t, filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "work"), applier)

	if err := service.Apply(t.Context(), testBundle()); err == nil {
		t.Fatal("Apply() error = nil, want adapter error")
	}
	if _, err := os.Stat(applier.payloadPath); err != nil {
		t.Fatalf("payload original was not preserved: %v", err)
	}
	if _, err := service.readMarker(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker exists after failed apply: err=%v", err)
	}
}

func TestServiceは同じDigestの成功MarkerがあればBootstrapを再適用しない(t *testing.T) {
	applier := &recordingApplier{}
	service := newTestService(t, filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "work"), applier)
	bundle := testBundle()
	if err := service.Apply(t.Context(), bundle); err != nil {
		t.Fatalf("Apply(first) error = %v", err)
	}
	applier.calls = 0

	if err := service.Apply(t.Context(), bundle); err != nil {
		t.Fatalf("Apply(second) error = %v", err)
	}
	if applier.calls != 0 {
		t.Fatalf("applier calls = %d, want 0", applier.calls)
	}
}

func newTestService(t *testing.T, stateDir, workDir string, applier Applier) *Service {
	t.Helper()
	service, err := NewService(stateDir, workDir, applier, func() time.Time {
		return time.Date(2026, 7, 12, 3, 4, 5, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testBundle() agentprotocol.BootstrapBundle {
	payload := []byte("#cloud-config\n")
	return agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       payload,
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    "machine-uid",
		OperationUID:  "operation-uid",
	}
}

type recordingApplier struct {
	calls       int
	payloadPath string
	err         error
}

func (applier *recordingApplier) ApplyCloudConfig(
	_ context.Context,
	payloadPath string,
	_ agentprotocol.BootstrapBundle,
) error {
	applier.calls++
	applier.payloadPath = payloadPath
	data, err := os.ReadFile(payloadPath)
	if err != nil {
		return err
	}
	if string(data) != "#cloud-config\n" {
		return errors.New("unexpected payload")
	}
	return applier.err
}
