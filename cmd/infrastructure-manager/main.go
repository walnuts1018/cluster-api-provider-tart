// Command infrastructure-managerはinfrastructure.cluster.x-k8s.io groupのTartHost、TartCluster、
// TartMachine、TalosRecovery reconcilerを実行するcontroller-managerである。cluster-api-operator/
// clusterctlからBootstrap/ControlPlane providerと独立してインストール・更新できるよう、別binary/
// imageとして分離している。
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
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/internal/managersetup"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/kessoku"
	// +kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(infrav1alpha1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha1.AddToScheme(scheme))
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

	mgr, err := managersetup.NewManager(scheme, "infrastructure-tart.cluster.x-k8s.io", f)
	if err != nil {
		managersetup.Exit(err, "Failed to start manager")
	}

	reconcilers := kessoku.InitializeInfrastructureReconcilers(mgr.GetClient())
	managementNamespace := os.Getenv("POD_NAMESPACE")
	reconcilers.TartHost.ManagementNamespace = managementNamespace
	reconcilers.TartMachine.ManagementNamespace = managementNamespace
	reconcilers.TalosRecovery.ManagementNamespace = managementNamespace

	if err := reconcilers.TartHost.SetupWithManager(mgr); err != nil {
		managersetup.Log.Error(err, "Failed to create controller", "controller", "TartHost")
		os.Exit(1)
	}
	if err := reconcilers.TartCluster.SetupWithManager(mgr); err != nil {
		managersetup.Log.Error(err, "Failed to create controller", "controller", "TartCluster")
		os.Exit(1)
	}
	if err := reconcilers.TartMachine.SetupWithManager(mgr); err != nil {
		managersetup.Log.Error(err, "Failed to create controller", "controller", "TartMachine")
		os.Exit(1)
	}
	if err := reconcilers.TalosRecovery.SetupWithManager(mgr); err != nil {
		managersetup.Log.Error(err, "Failed to create controller", "controller", "TalosRecovery")
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
