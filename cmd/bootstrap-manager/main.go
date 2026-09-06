// Command bootstrap-managerはbootstrap.cluster.x-k8s.io groupのTartBootstrapConfig reconcilerを
// 実行するcontroller-managerである。cluster-api-operator/clusterctlからInfrastructure/ControlPlane
// providerと独立してインストール・更新できるよう、別binary/imageとして分離している。
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

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/internal/managersetup"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/kessoku"
	// +kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1alpha1.AddToScheme(scheme))
	utilruntime.Must(infrav1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	f := managersetup.BindFlags()
	flag.Parse()
	f.Normalize()

	ctx := context.Background()

	otelProvider, err := managersetup.NewTelemetryProvider(ctx)
	if err != nil {
		managersetup.Exit(err, "Failed to create OpenTelemetry provider")
	}

	managersetup.SetupLogging(f)

	mgr, err := managersetup.NewManager(scheme, "bootstrap-tart.cluster.x-k8s.io", f)
	if err != nil {
		managersetup.Exit(err, "Failed to start manager")
	}

	reconcilers := kessoku.InitializeBootstrapReconcilers(mgr.GetClient())

	if err := reconcilers.TartBootstrapConfig.SetupWithManager(mgr); err != nil {
		managersetup.Log.Error(err, "Failed to create controller", "controller", "TartBootstrapConfig")
		os.Exit(1)
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
