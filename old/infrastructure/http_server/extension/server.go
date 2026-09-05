package extension

import (
	"context"
	"fmt"
	"os"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
	"sigs.k8s.io/cluster-api/exp/runtime/server"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Manager runs the Runtime Extension webhook server.
type Manager struct {
	server *server.Server
}

// NewManager creates a new Runtime Extension server manager.
func NewManager(
	catalog *runtimecatalog.Catalog,
	reader client.Reader,
	updateStarter UpdateStarter,
	gates UpdateTargetFeatureGates,
	nodeLifecycleGates NodeLifecycleFeatureGates,
) (*Manager, error) {
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
	support := NewTargetSupportChecker(reader, gates, nodeLifecycleGates)
	if err := s.AddExtensionHandler(server.ExtensionHandler{
		Hook:           runtimehooksv1.CanUpdateMachine,
		HandlerFunc:    NewCanUpdateMachineHandler(support).Handle,
		Name:           "can-update-machine",
		TimeoutSeconds: new(int32(10)),
	}); err != nil {
		return nil, fmt.Errorf("failed to register CanUpdateMachine handler: %w", err)
	}

	if err := s.AddExtensionHandler(server.ExtensionHandler{
		Hook:           runtimehooksv1.CanUpdateMachineSet,
		HandlerFunc:    NewCanUpdateMachineSetHandler(support).Handle,
		Name:           "can-update-machine-set",
		TimeoutSeconds: new(int32(10)),
	}); err != nil {
		return nil, fmt.Errorf("failed to register CanUpdateMachineSet handler: %w", err)
	}

	if err := s.AddExtensionHandler(server.ExtensionHandler{
		Hook:           runtimehooksv1.UpdateMachine,
		HandlerFunc:    NewUpdateMachineHandlerWithSupport(updateStarter, support).Handle,
		Name:           "update-machine",
		TimeoutSeconds: new(int32(10)),
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
