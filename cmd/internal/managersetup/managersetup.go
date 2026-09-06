// Package managersetupは、bootstrap/control-plane/infrastructureの3つのcontroller-manager entrypointが
// 共通して必要とする初期化処理(flag定義、OpenTelemetry provider生成、logger設定、HTTP/2無効化、
// metrics server options構築、manager起動、health/readyz probe登録、shutdown処理)を提供する。
// scheme登録、reconciler登録、Runtime Extension登録はprovider固有のためここには含めない。
package managersetup

import (
	"context"
	"crypto/tls"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/go-logr/logr"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	applogger "github.com/walnuts1018/cluster-api-provider-tart/utils/logger"
	"github.com/walnuts1018/cluster-api-provider-tart/utils/telemetry"
)

// Logは各entrypointのsetup処理で共通して使うloggerである。
var Log = ctrl.Log.WithName("setup")

// Flagsは3つのmanagerで共通するcommand line flagを保持する。
// Runtime Extension関連のflagはcontrol-plane-managerのみが必要とするため、
// このstructには含めずcontrol-plane-manager側で個別に定義する。
type Flags struct {
	MetricsAddr          string
	DiagnosticsAddr      string
	InsecureDiagnostics  bool
	ProbeAddr            string
	EnableLeaderElection bool
	SecureMetrics        bool
	MetricsCertPath      string
	MetricsCertName      string
	MetricsCertKey       string
	EnableHTTP2          bool
	LogLevel             string
	LogType              string
}

// BindFlagsは共通flagをflag.CommandLineへ登録する。flag.Parse()は呼び出し元(各main関数)が
// 自身のprovider固有flagを追加登録した後にまとめて実行する。
func BindFlags() *Flags {
	f := &Flags{}

	flag.StringVar(&f.MetricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&f.DiagnosticsAddr, "diagnostics-address", "", "The address the diagnostics endpoint binds to. "+
		"If set, this address will be used for metrics and profiling.")
	flag.BoolVar(&f.InsecureDiagnostics, "insecure-diagnostics", false, "Enable insecure diagnostics.")
	flag.StringVar(&f.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&f.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&f.SecureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&f.MetricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&f.MetricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&f.MetricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&f.EnableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics server")
	flag.StringVar(&f.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&f.LogType, "log-type", "json", "Log type (json, text)")

	return f
}

// Normalizeはflag.Parse()後に、diagnostics-address/insecure-diagnosticsを他のflagへ反映する。
func (f *Flags) Normalize() {
	if f.DiagnosticsAddr != "" {
		f.MetricsAddr = f.DiagnosticsAddr
	}
	if f.InsecureDiagnostics {
		f.SecureMetrics = false
	}
}

// SetupLoggingは指定されたlog level/typeでslog/logr/klog/controller-runtimeのloggerを設定する。
func SetupLogging(f *Flags) {
	logger := applogger.Create(f.LogLevel, f.LogType)
	logrLogger := logr.FromSlogHandler(logger.Handler())
	slog.SetDefault(logger)
	klog.SetLogger(logrLogger)
	ctrl.SetLogger(logrLogger)
}

// NewManagerは共通のmanager options(metrics server, health probe, leader election, HTTP/2無効化)を
// 組み立ててcontroller-runtime managerを生成する。schemeとleaderElectionIDはprovider固有のため引数で受け取る。
func NewManager(scheme *runtime.Scheme, leaderElectionID string, f *Flags) (ctrl.Manager, error) {
	var tlsOpts []func(*tls.Config)

	// enable-http2 flagがfalse(既定値)の場合は脆弱性を持つHTTP/2を無効化する。これによりHTTP/2 Stream CancellationとRapid ResetのCVEに該当する状態を避ける。詳細は次のCVE advisoryを参照する。
	// https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		Log.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !f.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Metrics endpointはconfig/default/kustomization.yamlで有効化する。Metrics optionsはserverを設定する。詳細は次のdocumentationを参照する。
	// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/metrics/server
	// https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   f.MetricsAddr,
		SecureServing: f.SecureMetrics,
		TLSOpts:       tlsOpts,
	}

	if f.SecureMetrics {
		// FilterProviderはmetrics endpointをauthn/authzで保護するために使用する。
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(f.MetricsCertPath) > 0 {
		Log.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", f.MetricsCertPath, "metrics-cert-name", f.MetricsCertName, "metrics-cert-key", f.MetricsCertKey)

		metricsServerOptions.CertDir = f.MetricsCertPath
		metricsServerOptions.CertName = f.MetricsCertName
		metricsServerOptions.KeyName = f.MetricsCertKey
	}

	var pprofAddr string
	if f.DiagnosticsAddr != "" {
		pprofAddr = f.DiagnosticsAddr
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: f.ProbeAddr,
		PprofBindAddress:       pprofAddr,
		LeaderElection:         f.EnableLeaderElection,
		LeaderElectionID:       leaderElectionID,
		Client: client.Options{
			// Kubernetes upgradeの排他はLeaseのresourceVersion CASで確立するため、cacheした古いresourceVersionを使わない。
			Cache: &client.CacheOptions{DisableFor: []client.Object{&coordinationv1.Lease{}}},
		},
	})
	if err != nil {
		return nil, err
	}

	return mgr, nil
}

// AddHealthChecksはhealthz/readyz probeをmanagerへ登録する。
func AddHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	return nil
}

// RunはOpenTelemetry providerを起動した上でmanagerをstartし、終了時にproviderをshutdownする。
// manager起動が失敗した場合もshutdownは必ず実行してからerrorを返す。shutdownはOS signalによる
// 上位contextのcancel後に実行されるため、外部contextを引き継がず独立したtimeoutを持たせる。
func Run(mgr ctrl.Manager, otelProvider *telemetry.Provider) error {
	Log.Info("Starting manager")
	startErr := mgr.Start(ctrl.SetupSignalHandler())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := otelProvider.Shutdown(shutdownCtx); err != nil {
		Log.Error(err, "Failed to shutdown OpenTelemetry provider")
	}
	cancel()

	return startErr
}

// NewTelemetryProviderはOpenTelemetry providerを生成する。生成失敗はプロセス起動を継続できない致命的な
// 事象なので、呼び出し元でエラーを受けたらos.Exitすることを想定する。
func NewTelemetryProvider(ctx context.Context) (*telemetry.Provider, error) {
	return telemetry.NewProvider(ctx)
}

// Exitはsetup処理中のエラーをlogへ出力してプロセスを終了する。
func Exit(err error, msg string) {
	Log.Error(err, msg)
	os.Exit(1)
}
