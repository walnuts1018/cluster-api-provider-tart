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
	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/wire"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/controller"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/bootstrapper"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/ipxe"
	applogger "github.com/walnuts1018/cluster-api-provider-tart/pkg/logger"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(infrastructurev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var ipxeBindAddress string
	var bootstrapBindAddress string
	var bootstrapAdvertiseAddress string
	var tftpBindAddress string
	var assetsRoot string
	var tftpRoot string
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var logLevelStr string
	var logTypeStr string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&ipxeBindAddress, "ipxe-bind-address", ":8082", "The address the iPXE script endpoint binds to. Use 0 to disable.")
	flag.StringVar(&bootstrapBindAddress, "bootstrap-bind-address", ":67", "The address the bootstrap (ProxyDHCP) server binds to. Use 0 to disable.")
	flag.StringVar(&bootstrapAdvertiseAddress, "bootstrap-advertise-address", "", "The reachable IP address advertised to PXE/iPXE clients. Leave empty to auto-detect.")
	flag.StringVar(&tftpBindAddress, "tftp-bind-address", ":69", "The address the TFTP server binds to.")
	flag.StringVar(&assetsRoot, "assets-root", "/var/lib/tart/assets", "The root directory for HTTP-served boot assets.")
	flag.StringVar(&tftpRoot, "tftp-root", "/var/lib/tftpboot", "The root directory for TFTP server.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&logLevelStr, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&logTypeStr, "log-type", "json", "Log type (json, text)")
	flag.Parse()

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

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "987dfa6a.cluster.x-k8s.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &infrastructurev1alpha1.TartHost{}, "spec.macAddress", func(rawObj client.Object) []string {
		host := rawObj.(*infrastructurev1alpha1.TartHost)
		if mac, err := ipxe.NormalizeMAC(host.Spec.MACAddress); err == nil {
			return []string{mac}
		}
		return nil
	}); err != nil {
		setupLog.Error(err, "Failed to create index for TartHost MACAddress")
		os.Exit(1)
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &infrastructurev1alpha1.TartHost{}, "spec.bootMACAddress", func(rawObj client.Object) []string {
		host := rawObj.(*infrastructurev1alpha1.TartHost)
		if host.Spec.BootMACAddress != "" {
			if mac, err := ipxe.NormalizeMAC(host.Spec.BootMACAddress); err == nil {
				return []string{mac}
			}
		}
		return nil
	}); err != nil {
		setupLog.Error(err, "Failed to create index for TartHost BootMACAddress")
		os.Exit(1)
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &infrastructurev1alpha1.TartHost{}, "status.machineRef", controller.IndexTartHostByMachineRef); err != nil {
		setupLog.Error(err, "Failed to create index for TartHost MachineRef")
		os.Exit(1)
	}

	reconcilers, err := wire.InitializeReconcilers(mgr.GetClient(), mgr.GetScheme())
	if err != nil {
		setupLog.Error(err, "Failed to initialize reconcilers")
		os.Exit(1)
	}

	if err := reconcilers.TartHost.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartHost")
		os.Exit(1)
	}
	if err := reconcilers.TartMachine.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartMachine")
		os.Exit(1)
	}
	if err := reconcilers.TartCluster.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartCluster")
		os.Exit(1)
	}
	if err := reconcilers.TartMachineTemplate.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartMachineTemplate")
		os.Exit(1)
	}
	if ipxeBindAddress != "0" {
		if err := mgr.Add(ipxe.NewServer(mgr.GetClient(), ipxeBindAddress, assetsRoot)); err != nil {
			setupLog.Error(err, "Failed to add iPXE server")
			os.Exit(1)
		}
	}
	if bootstrapBindAddress != "0" {
		bs, err := bootstrapper.NewCombinedBootstrapper(tftpRoot, bootstrapBindAddress, tftpBindAddress, ipxeBindAddress, bootstrapAdvertiseAddress)
		if err != nil {
			setupLog.Error(err, "Failed to create bootstrap server")
			os.Exit(1)
		}
		if err := mgr.Add(bs); err != nil {
			setupLog.Error(err, "Failed to add bootstrap server")
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
