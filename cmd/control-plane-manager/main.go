// Command control-plane-managerはcontrolplane.cluster.x-k8s.io groupのTartControlPlane reconcilerと、
// Talos in-place update用のCAPI Runtime Extensionを実行するcontroller-managerである。
// cluster-api-operator/clusterctlからBootstrap/Infrastructure providerと独立してインストール・更新
// できるよう、別binary/imageとして分離している。
package main

import (
	"context"
	"flag"
	"os"

	// Kubernetes clientの全auth pluginをimportし、exec-entrypointとrunから利用できるようにする。
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/internal/managersetup"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/kessoku"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/runtimeextension"
	// +kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	f := managersetup.BindFlags()

	var enableRuntimeExtension bool
	var runtimeExtensionCertPath string
	flag.BoolVar(&enableRuntimeExtension, "enable-runtime-extension", false,
		"Enable the CAPI Runtime Extension HTTPS server for Talos in-place update hooks.")
	flag.StringVar(&runtimeExtensionCertPath, "runtime-extension-cert-path", "",
		"The directory that contains the Runtime Extension server certificate.")

	flag.Parse()
	f.Normalize()

	ctx := context.Background()

	otelProvider, err := managersetup.NewTelemetryProvider(ctx)
	if err != nil {
		managersetup.Exit(err, "Failed to create OpenTelemetry provider")
	}

	managersetup.SetupLogging(f)

	mgr, err := managersetup.NewManager(scheme, "control-plane-tart.cluster.x-k8s.io", f)
	if err != nil {
		managersetup.Exit(err, "Failed to start manager")
	}

	reconcilers := kessoku.InitializeControlPlaneReconcilers(mgr.GetClient())

	if err := reconcilers.TartControlPlane.SetupWithManager(mgr); err != nil {
		managersetup.Log.Error(err, "Failed to create controller", "controller", "TartControlPlane")
		os.Exit(1)
	}

	if enableRuntimeExtension {
		catalog, err := runtimeextension.NewCatalog()
		if err != nil {
			managersetup.Exit(err, "Failed to create Runtime Extension catalog")
		}
		extManager, err := runtimeextension.NewManager(catalog, runtimeExtensionCertPath, mgr.GetClient())
		if err != nil {
			managersetup.Exit(err, "Failed to create Runtime Extension manager")
		}
		if err := mgr.Add(extManager); err != nil {
			managersetup.Exit(err, "Failed to add Runtime Extension manager")
		}
	}
	// +kubebuilder:scaffold:builder

	if err := managersetup.AddHealthChecks(mgr); err != nil {
		managersetup.Exit(err, "Failed to set up health check")
	}

	if err := managersetup.Run(mgr, otelProvider); err != nil {
		managersetup.Log.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}
