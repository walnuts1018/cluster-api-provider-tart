package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log/slog"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/kessoku"
	"github.com/walnuts1018/cluster-api-provider-tart/extensions"
	applogger "github.com/walnuts1018/cluster-api-provider-tart/utils/logger"
	"github.com/walnuts1018/cluster-api-provider-tart/utils/telemetry"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(infrav1alpha1.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1alpha1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var enableLeaderElection bool
	var enableRuntimeExtension bool
	var runtimeExtensionCertPath string
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var logLevelStr string
	var logTypeStr string
	var diagnosticsAddr string
	var insecureDiagnostics bool
	var tlsOpts []func(*tls.Config)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&diagnosticsAddr, "diagnostics-address", "", "The address the diagnostics endpoint binds to. "+
		"If set, this address will be used for metrics and profiling.")
	flag.BoolVar(&insecureDiagnostics, "insecure-diagnostics", false, "Enable insecure diagnostics.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableRuntimeExtension, "enable-runtime-extension", false,
		"Enable the CAPI Runtime Extension HTTPS server for in-place update hooks. "+
			"Every hook currently returns Failure; see extensions package documentation.")
	flag.StringVar(&runtimeExtensionCertPath, "runtime-extension-cert-path", "",
		"The directory that contains the Runtime Extension server certificate.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics server")
	flag.StringVar(&logLevelStr, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&logTypeStr, "log-type", "json", "Log type (json, text)")
	flag.Parse()

	if diagnosticsAddr != "" {
		metricsAddr = diagnosticsAddr
	}
	if insecureDiagnostics {
		secureMetrics = false
	}

	ctx := context.Background()

	otelProvider, err := telemetry.NewProvider(ctx)
	if err != nil {
		setupLog.Error(err, "Failed to create OpenTelemetry provider")
		os.Exit(1)
	}

	logger := applogger.Create(logLevelStr, logTypeStr)
	logrLogger := logr.FromSlogHandler(logger.Handler())
	slog.SetDefault(logger)
	klog.SetLogger(logrLogger)
	ctrl.SetLogger(logrLogger)

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	var pprofAddr string
	if diagnosticsAddr != "" {
		pprofAddr = diagnosticsAddr
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: probeAddr,
		PprofBindAddress:       pprofAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "987dfa6a.cluster.x-k8s.io",
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	reconcilers := kessoku.InitializeReconcilers(mgr.GetClient())

	if err := reconcilers.TartHost.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartHost")
		os.Exit(1)
	}
	if err := reconcilers.TartCluster.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartCluster")
		os.Exit(1)
	}
	if err := reconcilers.TartMachine.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartMachine")
		os.Exit(1)
	}
	if err := reconcilers.TartBootstrapConfig.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartBootstrapConfig")
		os.Exit(1)
	}
	if err := reconcilers.TartControlPlane.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartControlPlane")
		os.Exit(1)
	}

	if enableRuntimeExtension {
		catalog, err := extensions.NewCatalog()
		if err != nil {
			setupLog.Error(err, "Failed to create Runtime Extension catalog")
			os.Exit(1)
		}
		extManager, err := extensions.NewManager(catalog, runtimeExtensionCertPath)
		if err != nil {
			setupLog.Error(err, "Failed to create Runtime Extension manager")
			os.Exit(1)
		}
		if err := mgr.Add(extManager); err != nil {
			setupLog.Error(err, "Failed to add Runtime Extension manager")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	startErr := mgr.Start(ctrl.SetupSignalHandler())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := otelProvider.Shutdown(shutdownCtx); err != nil {
		setupLog.Error(err, "Failed to shutdown OpenTelemetry provider")
	}
	cancel()

	if startErr != nil {
		setupLog.Error(startErr, "Failed to run manager")
		os.Exit(1)
	}
}
