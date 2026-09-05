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

package extensions

import (
	"context"
	"fmt"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
	"sigs.k8s.io/cluster-api/exp/runtime/server"
	ctrl "sigs.k8s.io/controller-runtime"
)

const handlerTimeoutSeconds = 10

// Manager runs the Runtime Extension HTTPS server registered with CAPI's
// ExtensionConfig.
type Manager struct {
	server *server.Server
}

// NewManager creates a Runtime Extension server with the in-place update hooks wired
// to safe-veto handlers. See package doc for why every handler returns Failure today.
func NewManager(catalog *runtimecatalog.Catalog, certDir string) (*Manager, error) {
	s, err := server.New(server.Options{
		Catalog:  catalog,
		Port:     9443,
		CertDir:  certDir,
		CertName: "tls.crt",
		KeyName:  "tls.key",
	})
	if err != nil {
		return nil, fmt.Errorf("create runtime extension server: %w", err)
	}

	timeout := int32(handlerTimeoutSeconds)
	handlers := []server.ExtensionHandler{
		{
			Hook:           runtimehooksv1.CanUpdateMachine,
			HandlerFunc:    canUpdateMachine,
			Name:           "can-update-machine",
			TimeoutSeconds: &timeout,
		},
		{
			Hook:           runtimehooksv1.CanUpdateMachineSet,
			HandlerFunc:    canUpdateMachineSet,
			Name:           "can-update-machine-set",
			TimeoutSeconds: &timeout,
		},
		{
			Hook:           runtimehooksv1.UpdateMachine,
			HandlerFunc:    updateMachine,
			Name:           "update-machine",
			TimeoutSeconds: &timeout,
		},
	}
	for _, h := range handlers {
		if err := s.AddExtensionHandler(h); err != nil {
			return nil, fmt.Errorf("register %s handler: %w", h.Name, err)
		}
	}

	return &Manager{server: s}, nil
}

// Start starts the Runtime Extension HTTPS server. It implements
// sigs.k8s.io/controller-runtime/pkg/manager.Runnable so it can be added to the
// controller-manager's manager.
func (m *Manager) Start(ctx context.Context) error {
	ctrl.Log.Info("Starting Runtime Extension server")
	return m.server.Start(ctx)
}
