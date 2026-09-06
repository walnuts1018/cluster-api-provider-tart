package extensions

import (
	"context"
	"fmt"

	runtimecatalog "sigs.k8s.io/cluster-api/api/runtime/catalog"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/runtime/server"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const handlerTimeoutSeconds = 10

// ManagerはCAPIのExtensionConfigへ登録するRuntime Extension HTTPS serverを実行する。
type Manager struct {
	server *server.Server
}

// NewManagerはin-place update hookをTalos OS update handlerへ接続したRuntime Extension serverを生成する。Kubernetes readerはUpdateMachineが実際のHost、Secret、provider resourceを解決するために使用する。
func NewManager(catalog *runtimecatalog.Catalog, certDir string, readers ...client.Reader) (*Manager, error) {
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
	var reader client.Reader
	if len(readers) > 1 {
		return nil, fmt.Errorf("create runtime extension server: at most one Kubernetes reader is supported")
	}
	if len(readers) == 0 || readers[0] == nil {
		return nil, fmt.Errorf("create runtime extension server: a Kubernetes reader is required for UpdateMachine")
	}
	reader = readers[0]
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
			HandlerFunc:    newUpdateMachineHandler(reader),
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

// StartはRuntime Extension HTTPS serverを開始する。controller-managerのmanagerへ追加できるよう、sigs.k8s.io/controller-runtime/pkg/manager.Runnableを実装する。
func (m *Manager) Start(ctx context.Context) error {
	ctrl.Log.Info("Starting Runtime Extension server")
	return m.server.Start(ctx)
}
