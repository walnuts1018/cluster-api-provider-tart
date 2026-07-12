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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const (
	payloadFileName = "payload.cloud-config"
	markerFileName  = "applied.json"
)

type Applier interface {
	ApplyCloudConfig(context.Context, string, agentprotocol.BootstrapBundle) error
}

type Clock func() time.Time

type Service struct {
	stateDir string
	workDir  string
	applier  Applier
	now      Clock
}

type AppliedMarker struct {
	APIVersion     string    `json:"apiVersion"`
	Format         string    `json:"format"`
	PayloadDigest  string    `json:"payloadDigest"`
	MachineUID     string    `json:"machineUID"`
	OperationUID   string    `json:"operationUID"`
	AdapterVersion string    `json:"adapterVersion"`
	AppliedAt      time.Time `json:"appliedAt"`
}

func NewService(stateDir, workDir string, applier Applier, now Clock) (*Service, error) {
	switch {
	case stateDir == "":
		return nil, errors.New("state directory is required")
	case workDir == "":
		return nil, errors.New("bootstrap work directory is required")
	case applier == nil:
		return nil, errors.New("bootstrap applier is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		stateDir: stateDir,
		workDir:  workDir,
		applier:  applier,
		now:      now,
	}, nil
}

func (service *Service) Applied(operationUID string) (bool, error) {
	marker, err := service.readMarker()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return marker.OperationUID == operationUID, nil
}

func (service *Service) Apply(ctx context.Context, bundle agentprotocol.BootstrapBundle) error {
	if err := agentprotocol.ValidateBootstrapBundle(bundle); err != nil {
		return fmt.Errorf("validate Bootstrap Bundle: %w", err)
	}
	marker, err := service.readMarker()
	if err == nil && marker.OperationUID == bundle.OperationUID && marker.PayloadDigest == bundle.PayloadDigest {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payloadPath := filepath.Join(service.operationWorkDir(bundle.OperationUID), payloadFileName)
	if err := service.writePayload(payloadPath, bundle.Payload); err != nil {
		return err
	}
	if err := service.applier.ApplyCloudConfig(ctx, payloadPath, bundle); err != nil {
		return fmt.Errorf("apply cloud-config bootstrap: %w", err)
	}
	if err := service.writeMarker(AppliedMarker{
		APIVersion:     agentprotocol.APIVersion,
		Format:         bundle.Format,
		PayloadDigest:  bundle.PayloadDigest,
		MachineUID:     bundle.MachineUID,
		OperationUID:   bundle.OperationUID,
		AdapterVersion: "cloud-config/v1",
		AppliedAt:      service.now().UTC(),
	}); err != nil {
		return err
	}
	if err := os.Remove(payloadPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove applied bootstrap payload: %w", err)
	}
	return nil
}

func (service *Service) operationWorkDir(operationUID string) string {
	return filepath.Join(service.workDir, operationUID)
}

func (service *Service) markerPath() string {
	return filepath.Join(service.stateDir, "bootstrap", markerFileName)
}

func (service *Service) readMarker() (AppliedMarker, error) {
	data, err := os.ReadFile(service.markerPath())
	if err != nil {
		return AppliedMarker{}, err
	}
	var marker AppliedMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return AppliedMarker{}, fmt.Errorf("decode bootstrap success marker: %w", err)
	}
	return marker, nil
}

func (service *Service) writePayload(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create bootstrap payload directory: %w", err)
	}
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create bootstrap payload: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		closeErr := file.Close()
		return errors.Join(fmt.Errorf("write bootstrap payload: %w", err), closeErr)
	}
	if err := file.Sync(); err != nil {
		closeErr := file.Close()
		return errors.Join(fmt.Errorf("fsync bootstrap payload: %w", err), closeErr)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close bootstrap payload: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit bootstrap payload: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func (service *Service) writeMarker(marker AppliedMarker) error {
	path := service.markerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create bootstrap marker directory: %w", err)
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bootstrap success marker: %w", err)
	}
	data = append(data, '\n')
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write bootstrap success marker: %w", err)
	}
	file, err := os.OpenFile(tmpPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open bootstrap success marker for fsync: %w", err)
	}
	if err := file.Sync(); err != nil {
		closeErr := file.Close()
		return errors.Join(fmt.Errorf("fsync bootstrap success marker: %w", err), closeErr)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close bootstrap success marker: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit bootstrap success marker: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for fsync: %w", err)
	}
	if err := dir.Sync(); err != nil {
		closeErr := dir.Close()
		return errors.Join(fmt.Errorf("fsync directory: %w", err), closeErr)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("close directory after fsync: %w", err)
	}
	return nil
}
