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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/cmd/wire"
	k8sagentapi "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/agentapi"
	k8sagentboot "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/agentboot"
	k8sagentprogress "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/agentprogress"
	k8sagentsession "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/agentsession"
	k8sbootreport "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/bootreport"
	k8sdistributionlifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/distributionlifecycle"
	k8sinplaceupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/inplaceupdate"
	k8snodelifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/nodelifecycle"
	k8soperation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/operation"
	k8sv1beta1host "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/v1beta1host"
	applicationcleaning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	applicationinplaceupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/controller"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/artifactfetch"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/agentapi"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/agentboot"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/bootstrapper"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/extension"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/ipxe"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/signingkey"
	webhookv1beta1 "github.com/walnuts1018/cluster-api-provider-tart/internal/webhook/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
	applogger "github.com/walnuts1018/cluster-api-provider-tart/pkg/logger"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
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
	utilruntime.Must(infrastructurev1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

//nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var ipxeBindAddress string
	var ipxeDomain string
	var bootstrapBindAddress string
	var bootstrapAdvertiseAddress string
	var tftpBindAddress string
	var assetsRoot string
	var tftpRoot string
	var agentAPIBindAddress string
	var agentAPICertFile string
	var agentAPIKeyFile string
	var agentAPIAllowIsolatedL2 bool
	var agentAPIURL string
	var agentArtifactRoot string
	var agentArtifactKeyID string
	var agentArtifactPublicKeyFile string
	var agentArtifactBaseURL string
	var agentBootCertFile string
	var agentBootKeyFile string
	var osArtifactKeyID string
	var osArtifactPublicKeyFile string
	var agentPlanKeyID string
	var agentPlanPrivateKeyFile string
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var logLevelStr string
	var logTypeStr string
	var diagnosticsAddr string
	var insecureDiagnostics bool
	var featureGatesStr string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&diagnosticsAddr, "diagnostics-address", "", "The address the diagnostics endpoint binds to. "+
		"If set, this address will be used for metrics and profiling.")
	flag.BoolVar(&insecureDiagnostics, "insecure-diagnostics", false, "Enable insecure diagnostics.")
	flag.StringVar(&featureGatesStr, "feature-gates", "", "Comma-separated list of key=value pairs that describe feature gates for alpha/experimental features.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&ipxeBindAddress, "ipxe-bind-address", ":8082", "The address the iPXE script endpoint binds to. Use 0 to disable.")
	flag.StringVar(&ipxeDomain, "ipxe-domain", "", "The domain to use for iPXE and metadata URLs (e.g. tart.example.com). If empty, the auto-detected IP address will be used.")
	flag.StringVar(&bootstrapBindAddress, "bootstrap-bind-address", "0.0.0.0", "The IP address the bootstrap (ProxyDHCP) server binds to. It will listen on both ports 67 and 4011. Use 0 to disable.")
	flag.StringVar(&bootstrapAdvertiseAddress, "bootstrap-advertise-address", "", "The reachable IP address advertised to PXE/iPXE clients. Leave empty to auto-detect.")
	flag.StringVar(&tftpBindAddress, "tftp-bind-address", ":69", "The address the TFTP server binds to.")
	flag.StringVar(&assetsRoot, "assets-root", "/var/lib/tart/assets", "The root directory for HTTP-served boot assets.")
	flag.StringVar(&tftpRoot, "tftp-root", "/var/lib/tftpboot", "The root directory for TFTP server.")
	flag.StringVar(&agentAPIBindAddress, "agent-api-bind-address", "0", "The HTTPS address the Agent API binds to. Use 0 to disable.")
	flag.StringVar(&agentAPICertFile, "agent-api-cert-file", "", "The Agent API TLS certificate file.")
	flag.StringVar(&agentAPIKeyFile, "agent-api-key-file", "", "The Agent API TLS private key file.")
	flag.StringVar(&agentAPIURL, "agent-api-url", "", "The HTTPS Agent API base URL advertised to the Provisioning Agent.")
	flag.StringVar(&agentArtifactRoot, "agent-artifact-root", "", "The verified Agent Artifact file root. Empty disables the v1 Agent boot server.")
	flag.StringVar(&agentArtifactKeyID, "agent-artifact-key-id", "", "The trusted Agent Artifact signing key ID.")
	flag.StringVar(&agentArtifactPublicKeyFile, "agent-artifact-public-key-file", "", "The trusted Agent Artifact Ed25519 public key file, mounted separately from the Artifact.")
	flag.StringVar(&agentArtifactBaseURL, "agent-artifact-base-url", "", "The HTTPS base URL used to deliver the Agent Artifact and iPXE script.")
	flag.StringVar(&agentBootCertFile, "agent-boot-cert-file", "", "The Agent boot HTTPS TLS certificate file.")
	flag.StringVar(&agentBootKeyFile, "agent-boot-key-file", "", "The Agent boot HTTPS TLS private key file.")
	flag.StringVar(&osArtifactKeyID, "os-artifact-key-id", "", "The trusted OS Artifact signing key ID used by in-place updates.")
	flag.StringVar(&osArtifactPublicKeyFile, "os-artifact-public-key-file", "", "The trusted OS Artifact Ed25519 public key file used by in-place updates.")
	flag.StringVar(&agentPlanKeyID, "agent-plan-key-id", "", "The Agent Plan signing key ID used by in-place updates.")
	flag.StringVar(&agentPlanPrivateKeyFile, "agent-plan-private-key-file", "", "The Agent Plan Ed25519 private key file, mounted read-only separately from Artifact trust keys.")
	flag.BoolVar(
		&agentAPIAllowIsolatedL2,
		"agent-api-allow-isolated-l2",
		false,
		"Allow unauthenticated initial registration from an isolated provisioning L2.",
	)
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

	featureGates, err := parseFeatureGates(featureGatesStr)
	if err != nil {
		setupLog.Error(err, "Failed to parse feature gates")
		os.Exit(1)
	}
	updateFeatureGates := resolveUpdateFeatureGates(featureGates)

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

	var pprofAddr string
	if diagnosticsAddr != "" {
		pprofAddr = diagnosticsAddr
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		PprofBindAddress:       pprofAddr,
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

	if agentAPIBindAddress != "0" {
		if agentAPICertFile == "" || agentAPIKeyFile == "" {
			setupLog.Error(nil, "Agent API TLS certificate and key are required")
			os.Exit(1)
		}
		if !agentAPIAllowIsolatedL2 {
			setupLog.Error(nil, "Agent API requires an explicitly selected initial credential mode")
			os.Exit(1)
		}
		if err := mgr.GetFieldIndexer().IndexField(
			context.Background(),
			&infrastructurev1beta1.TartHostOperation{},
			k8sagentapi.OperationIDField,
			k8sagentapi.OperationIDIndex,
		); err != nil {
			setupLog.Error(err, "Failed to create index for TartHostOperation operation ID")
			os.Exit(1)
		}
	}
	agentArtifactEnabled := agentArtifactRoot != ""
	if agentArtifactEnabled {
		required := map[string]string{
			"agent-api-url":                  agentAPIURL,
			"agent-artifact-key-id":          agentArtifactKeyID,
			"agent-artifact-public-key-file": agentArtifactPublicKeyFile,
			"agent-artifact-base-url":        agentArtifactBaseURL,
			"agent-boot-cert-file":           agentBootCertFile,
			"agent-boot-key-file":            agentBootKeyFile,
		}
		for name, value := range required {
			if value == "" {
				setupLog.Error(nil, "Agent Artifact delivery requires a configuration value", "flag", name)
				os.Exit(1)
			}
		}
		if ipxeBindAddress == "0" {
			setupLog.Error(nil, "Agent Artifact delivery requires the iPXE listener")
			os.Exit(1)
		}
		if agentAPIBindAddress == "0" {
			setupLog.Error(nil, "Agent Artifact delivery requires the Agent API")
			os.Exit(1)
		}
	}

	reconcilers, err := wire.InitializeReconcilers(mgr.GetClient(), mgr.GetScheme())
	if err != nil {
		setupLog.Error(err, "Failed to initialize reconcilers")
		os.Exit(1)
	}
	if agentPlanKeyID != "" && agentPlanPrivateKeyFile != "" {
		planPrivateKey, err := signingkey.LoadPrivateReadOnly(agentPlanPrivateKeyFile)
		if err != nil {
			setupLog.Error(err, "Failed to load Agent Plan signing key for Cleaning workflow")
			os.Exit(1)
		}
		cleaningOrchestrator := applicationcleaning.NewOrchestrator(
			k8sv1beta1host.NewService(mgr.GetClient()),
			k8soperation.NewService(mgr.GetClient()),
		)
		reconcilers.TartMachineV1Beta1.Cleaner = applicationcleaning.NewWorkflow(
			cleaningOrchestrator,
			k8sagentapi.NewPlanWriter(mgr.GetClient()),
			applicationcleaning.PlanSigner{
				KeyID:      agentPlanKeyID,
				PrivateKey: planPrivateKey,
			},
		)
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
	if err := reconcilers.TartMachineV1Beta1.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartMachineV1Beta1")
		os.Exit(1)
	}
	if err := reconcilers.TartHostOperation.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "TartHostOperation")
		os.Exit(1)
	}

	advertiseIP, err := bootstrapper.ResolveAdvertiseIP(bootstrapBindAddress, ipxeBindAddress, bootstrapAdvertiseAddress)
	if err != nil {
		setupLog.Error(err, "Failed to resolve advertise IP")
		os.Exit(1)
	}

	var baseURL string
	if agentArtifactEnabled {
		baseURL = strings.TrimSuffix(agentArtifactBaseURL, "/")
	} else if ipxeDomain != "" {
		u := url.URL{
			Scheme: "http",
			Host:   ipxeDomain,
		}
		baseURL = u.String()
	} else {
		_, port, err := net.SplitHostPort(ipxeBindAddress)
		if err != nil {
			port = "8082" // fallback
		}
		u := url.URL{
			Scheme: "http",
			Host:   net.JoinHostPort(advertiseIP.String(), port),
		}
		baseURL = u.String()
	}

	if ipxeBindAddress != "0" {
		if agentArtifactEnabled {
			virtualMediaPath, err := optionalFilePath(filepath.Join(agentArtifactRoot, "virtual-media.iso"))
			if err != nil {
				setupLog.Error(err, "Failed to inspect Agent Artifact virtual media")
				os.Exit(1)
			}
			artifact, err := agentboot.LoadArtifact(agentboot.ArtifactFiles{
				ManifestPath:     filepath.Join(agentArtifactRoot, "manifest.json"),
				SignaturePath:    filepath.Join(agentArtifactRoot, "manifest.signature.json"),
				KernelPath:       filepath.Join(agentArtifactRoot, "vmlinuz"),
				InitrdPath:       filepath.Join(agentArtifactRoot, "initrd"),
				VirtualMediaPath: virtualMediaPath,
				KeyID:            agentArtifactKeyID,
				PublicKeyPath:    agentArtifactPublicKeyFile,
			})
			if err != nil {
				setupLog.Error(err, "Failed to verify Agent Artifact")
				os.Exit(1)
			}
			if err := configureRedfishVirtualMedia(reconcilers.Driver, artifact, baseURL); err != nil {
				if closeErr := artifact.Close(); closeErr != nil {
					setupLog.Error(closeErr, "Failed to close Agent Artifact")
				}
				setupLog.Error(err, "Failed to configure Redfish VirtualMedia Agent Artifact")
				os.Exit(1)
			}
			handler, err := agentboot.NewHandler(agentboot.Config{
				Resolver:        k8sagentboot.NewResolver(mgr.GetClient()),
				Artifact:        artifact,
				ArtifactBaseURL: baseURL,
				AgentAPIURL:     agentAPIURL,
			})
			if err != nil {
				if closeErr := artifact.Close(); closeErr != nil {
					setupLog.Error(closeErr, "Failed to close Agent Artifact")
				}
				setupLog.Error(err, "Failed to create Agent boot handler")
				os.Exit(1)
			}
			if err := mgr.Add(agentboot.NewServer(
				ipxeBindAddress,
				agentBootCertFile,
				agentBootKeyFile,
				handler,
			)); err != nil {
				if closeErr := artifact.Close(); closeErr != nil {
					setupLog.Error(closeErr, "Failed to close Agent Artifact")
				}
				setupLog.Error(err, "Failed to add Agent boot server")
				os.Exit(1)
			}
		} else {
			if err := mgr.Add(ipxe.NewServer(mgr.GetClient(), reconcilers.TartMachine.TokenService, ipxeBindAddress, assetsRoot, baseURL)); err != nil {
				setupLog.Error(err, "Failed to add iPXE server")
				os.Exit(1)
			}
		}
	}
	if agentAPIBindAddress != "0" {
		provider := k8sagentapi.NewProvider(mgr.GetClient())
		handler := agentapi.NewHandler(agentapi.Config{
			Operations:           provider,
			RegistrationVerifier: agentapi.IsolatedL2RegistrationVerifier{},
			Sessions: k8sagentsession.NewService(
				mgr.GetClient(),
				agentsessiondomain.DefaultTTL,
			),
			Progress: k8sagentprogress.NewService(mgr.GetClient()),
			Plans:    provider,
			NodeLifecyclePlans: k8snodelifecycle.NewProvider(
				mgr.GetClient(),
			),
			NodeLifecycleStatus: k8sdistributionlifecycle.NewStatusStore(
				mgr.GetClient(),
			),
			Bootstrap: provider,
			BootReports: k8sbootreport.NewService(
				mgr.GetClient(),
				provider,
			),
		})
		if err := mgr.Add(agentapi.NewServer(
			agentAPIBindAddress,
			agentAPICertFile,
			agentAPIKeyFile,
			handler,
		)); err != nil {
			setupLog.Error(err, "Failed to add Agent API server")
			os.Exit(1)
		}
	}
	if bootstrapBindAddress != "0" {
		bs, err := bootstrapper.NewCombinedBootstrapper(tftpRoot, bootstrapBindAddress, tftpBindAddress, advertiseIP.String(), baseURL)
		if err != nil {
			setupLog.Error(err, "Failed to create bootstrap server")
			os.Exit(1)
		}
		if err := mgr.Add(bs); err != nil {
			setupLog.Error(err, "Failed to add bootstrap server")
			os.Exit(1)
		}
	}

	if updateFeatureGates.InPlaceUpdates {
		required := map[string]string{
			"os-artifact-key-id":          osArtifactKeyID,
			"os-artifact-public-key-file": osArtifactPublicKeyFile,
			"agent-plan-key-id":           agentPlanKeyID,
			"agent-plan-private-key-file": agentPlanPrivateKeyFile,
		}
		for name, value := range required {
			if value == "" {
				setupLog.Error(nil, "In-place updates require a configuration value", "flag", name)
				os.Exit(1)
			}
		}
		artifactPublicKey, err := signingkey.LoadPublic(osArtifactPublicKeyFile)
		if err != nil {
			setupLog.Error(err, "Failed to load OS Artifact verification key")
			os.Exit(1)
		}
		planPrivateKey, err := signingkey.LoadPrivateReadOnly(agentPlanPrivateKeyFile)
		if err != nil {
			setupLog.Error(err, "Failed to load Agent Plan signing key")
			os.Exit(1)
		}
		// TODO: private Registry credentialをcontroller設定へ追加した時点で匿名credentialを置き換える。
		manifestResolver, err := artifactfetch.NewOCI(
			artifact.StaticTrustStore{osArtifactKeyID: artifactPublicKey},
			nil,
		)
		if err != nil {
			setupLog.Error(err, "Failed to create OS Artifact Manifest resolver")
			os.Exit(1)
		}
		updateWorkflow := applicationinplaceupdate.NewWorkflow(
			k8soperation.NewService(mgr.GetClient()),
			k8sagentapi.NewPlanWriter(mgr.GetClient()),
			applicationinplaceupdate.PlanSigner{
				KeyID:      agentPlanKeyID,
				PrivateKey: planPrivateKey,
			},
		)
		updateWorkflow.SetNodeLifecyclePlanWriter(
			k8snodelifecycle.NewPlanWriter(mgr.GetClient()),
			applicationinplaceupdate.PlanSigner{
				KeyID:      agentPlanKeyID,
				PrivateKey: planPrivateKey,
			},
		)
		updateService := k8sinplaceupdate.NewService(
			mgr.GetClient(),
			manifestResolver,
			updateWorkflow,
		)
		extCatalog, err := extension.NewCatalog()
		if err != nil {
			setupLog.Error(err, "Failed to create Runtime Extension catalog")
			os.Exit(1)
		}
		extManager, err := extension.NewManager(extCatalog, updateService)
		if err != nil {
			setupLog.Error(err, "Failed to create Runtime Extension manager")
			os.Exit(1)
		}
		if err := mgr.Add(extManager); err != nil {
			setupLog.Error(err, "Failed to add Runtime Extension manager")
			os.Exit(1)
		}
	}

	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1beta1.SetupTartClusterWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "TartCluster")
			os.Exit(1)
		}
		if err := webhookv1beta1.SetupTartHostWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "TartHost")
			os.Exit(1)
		}
		if err := webhookv1beta1.SetupTartHostOperationWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "TartHostOperation")
			os.Exit(1)
		}
		if err := webhookv1beta1.SetupTartMachineWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "TartMachine")
			os.Exit(1)
		}
		if err := webhookv1beta1.SetupTartClusterTemplateWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "TartClusterTemplate")
			os.Exit(1)
		}
		if err := webhookv1beta1.SetupTartMachineTemplateWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "TartMachineTemplate")
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

func optionalFilePath(path string) (string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	return path, nil
}

func configureRedfishVirtualMedia(
	driverService *applicationdriver.Service,
	artifact agentboot.Artifact,
	baseURL string,
) error {
	if artifact.Manifest().VirtualMedia == nil {
		return nil
	}
	virtualMediaURL, err := artifact.VirtualMediaURL(baseURL)
	if err != nil {
		return err
	}
	provider, err := applicationdriver.NewStaticAgentArtifactProvider(virtualMediaURL)
	if err != nil {
		return err
	}
	driverService.SetAgentArtifactProvider(provider)
	return nil
}

type updateFeatureGates struct {
	InPlaceUpdates        bool
	Worker                bool
	MultiControlPlane     bool
	SingleControlPlane    bool
	DistributionLifecycle updateDistributionLifecycleFeatureGates
}

type updateDistributionLifecycleFeatureGates struct {
	Enabled            bool
	Worker             bool
	MultiControlPlane  bool
	SingleControlPlane bool
}

func resolveUpdateFeatureGates(gates map[string]bool) updateFeatureGates {
	if !gates["InPlaceUpdates"] {
		return updateFeatureGates{}
	}
	return updateFeatureGates{
		InPlaceUpdates:        true,
		Worker:                gates["InPlaceUpdatesWorker"],
		MultiControlPlane:     gates["InPlaceUpdatesMultiControlPlane"],
		SingleControlPlane:    gates["InPlaceUpdatesSingleControlPlane"],
		DistributionLifecycle: resolveDistributionLifecycleFeatureGates(gates),
	}
}

func resolveDistributionLifecycleFeatureGates(gates map[string]bool) updateDistributionLifecycleFeatureGates {
	if !gates["DistributionLifecycle"] {
		return updateDistributionLifecycleFeatureGates{}
	}
	worker := gates["DistributionLifecycleWorker"]
	multiControlPlane := worker && gates["DistributionLifecycleMultiControlPlane"]
	singleControlPlane := multiControlPlane && gates["DistributionLifecycleSingleControlPlane"]
	return updateDistributionLifecycleFeatureGates{
		Enabled:            true,
		Worker:             worker,
		MultiControlPlane:  multiControlPlane,
		SingleControlPlane: singleControlPlane,
	}
}

func parseFeatureGates(s string) (map[string]bool, error) {
	gates := make(map[string]bool)
	if s == "" {
		return gates, nil
	}
	for pair := range strings.SplitSeq(s, ",") {
		kv := strings.Split(pair, "=")
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid feature gate pair: %s", pair)
		}
		val, err := strconv.ParseBool(kv[1])
		if err != nil {
			return nil, fmt.Errorf("invalid value for feature gate %s: %w", kv[0], err)
		}
		gates[kv[0]] = val
	}
	return gates, nil
}
