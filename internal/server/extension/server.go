/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"context"
	"fmt"
	"os"

	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
	"sigs.k8s.io/cluster-api/exp/runtime/server"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Manager runs the Runtime Extension webhook server.
type Manager struct {
	server *server.Server
}

// NewManager creates a new Runtime Extension server manager.
func NewManager(catalog *runtimecatalog.Catalog) (*Manager, error) {
	s, err := server.New(server.Options{
		Catalog:  catalog,
		Port:     9443,
		CertDir:  getWebhookCertDir(),
		CertName: "tls.crt",
		KeyName:  "tls.key",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime extension server: %w", err)
	}

	// Register extension handlers for in-place update hooks.
	if err := s.AddExtensionHandler(server.ExtensionHandler{
		Hook:           HandleCanUpdateMachine,
		Name:           "can-update-machine",
		TimeoutSeconds: ptrInt32(10),
	}); err != nil {
		return nil, fmt.Errorf("failed to register CanUpdateMachine handler: %w", err)
	}

	if err := s.AddExtensionHandler(server.ExtensionHandler{
		Hook:           HandleCanUpdateMachineSet,
		Name:           "can-update-machine-set",
		TimeoutSeconds: ptrInt32(10),
	}); err != nil {
		return nil, fmt.Errorf("failed to register CanUpdateMachineSet handler: %w", err)
	}

	if err := s.AddExtensionHandler(server.ExtensionHandler{
		Hook:           HandleUpdateMachine,
		Name:           "update-machine",
		TimeoutSeconds: ptrInt32(10),
	}); err != nil {
		return nil, fmt.Errorf("failed to register UpdateMachine handler: %w", err)
	}

	return &Manager{server: s}, nil
}

// Start starts the Runtime Extension webhook server.
func (m *Manager) Start(ctx context.Context) error {
	ctrl.Log.Info("Starting Runtime Extension webhook server")
	return m.server.Start(ctx)
}

// getWebhookCertDir returns the webhook certificate directory.
// It reads from the WEBHOOK_CERT_DIR environment variable if set,
// otherwise falls back to the default controller-runtime cert directory.
func getWebhookCertDir() string {
	if dir := os.Getenv("WEBHOOK_CERT_DIR"); dir != "" {
		return dir
	}
	return ""
}

func ptrInt32(v int32) *int32 {
	return &v
}
