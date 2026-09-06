// Command netboot-serverは、controller-managerとは別processで動くProxyDHCP/TFTP/iPXEスクリプト配信サーバーである。
// 素のhostがWoL/Redfishで起動しPXE bootした際に、Talos maintenance modeへ到達させるための
// bootstrap adapterであり、docs/development/decisions.mdの方針に従いTartのResource modelの中心には置かない。
// netboot-serverはKubernetes APIをread-onlyで参照してTartHost/TartMachineからdesired imageを解決するため、
// clusterctlやcluster-api-operatorでInfrastructure Providerをインストールしただけでも(サイト固有の
// discovery image設定なしで)起動し、既にTartHost/TartMachineへclaimされたHostのPXE bootを処理できる。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot/dhcp"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot/httpboot"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
	applogger "github.com/walnuts1018/cluster-api-provider-tart/utils/logger"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(infrav1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		tftpRoot               string
		dhcpBindAddress        string
		tftpBindAddress        string
		httpBindAddress        string
		advertiseAddress       string
		advertiseHTTPBaseURL   string
		imageFactoryPXEBaseURL string
		discoveryTalosVersion  string
		discoverySchematicID   string
		logLevelStr            string
		logTypeStr             string
	)

	flag.StringVar(&tftpRoot, "tftp-root", "/var/lib/netboot-server/tftp", "iPXEブートローダを配置するTFTPルートディレクトリ")
	flag.StringVar(&dhcpBindAddress, "dhcp-bind-address", "0.0.0.0", "ProxyDHCP(port 67/4011)のbind address")
	flag.StringVar(&tftpBindAddress, "tftp-bind-address", "0.0.0.0:69", "TFTPサーバーのbind address")
	flag.StringVar(&httpBindAddress, "http-bind-address", ":8080", "iPXEスクリプト配信用HTTPサーバーのbind address")
	flag.StringVar(&advertiseAddress, "advertise-address", "", "PXEクライアントへ広告するサーバーIP(省略時は自動検出)")
	flag.StringVar(&advertiseHTTPBaseURL, "advertise-http-base-url", envOrDefault("TART_NETBOOT_ADVERTISE_HTTP_BASE_URL", ""), "iPXEスクリプトへ埋め込むHTTPサーバーのベースURL(例: http://192.0.2.10:8080)。省略時はbind addressから自動構成する")
	flag.StringVar(&imageFactoryPXEBaseURL, "image-factory-pxe-base-url", httpboot.ImageFactoryPXEBaseURLDefault, "Talos Image FactoryのPXE配信endpointのベースURL")
	flag.StringVar(&discoveryTalosVersion, "discovery-talos-version", envOrDefault("TART_NETBOOT_DISCOVERY_TALOS_VERSION", ""), "discovery boot用のTalos version(例: v1.11.2)。未設定の場合は既知のTartHost/TartMachineの解決のみ行う")
	flag.StringVar(&discoverySchematicID, "discovery-schematic-id", envOrDefault("TART_NETBOOT_DISCOVERY_SCHEMATIC_ID", ""), "discovery boot用のTalos Image Factory schematic ID")
	flag.StringVar(&logLevelStr, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&logTypeStr, "log-type", "json", "Log type (json, text)")
	flag.Parse()

	logger := applogger.Create(logLevelStr, logTypeStr)
	slog.SetDefault(logger)

	if discoveryTalosVersion == "" || discoverySchematicID == "" {
		logger.Warn("discovery Talos version/schematic ID is not configured; PXE requests from unregistered hosts will not receive a boot image until a TartHost is registered for their MAC address, or the discovery image is configured")
	}

	resolver, err := newResolver(logger)
	if err != nil {
		logger.Error("failed to create Kubernetes client for TartHost/TartMachine resolution; falling back to discovery image only", "error", err)
	}

	cfg := netboot.Config{
		TFTPRoot:               tftpRoot,
		DHCPBindAddress:        dhcpBindAddress,
		TFTPBindAddress:        tftpBindAddress,
		HTTPBindAddress:        httpBindAddress,
		AdvertiseAddress:       advertiseAddress,
		AdvertiseHTTPBaseURL:   advertiseHTTPBaseURL,
		ImageFactoryPXEBaseURL: imageFactoryPXEBaseURL,
		DiscoveryImage: domainnetboot.DiscoveryImage{
			Version:     discoveryTalosVersion,
			SchematicID: discoverySchematicID,
		},
		Resolver: resolver,
	}

	if cfg.AdvertiseHTTPBaseURL == "" {
		resolvedURL, err := dhcp.DefaultAdvertiseHTTPBaseURL(cfg.DHCPBindAddress, cfg.HTTPBindAddress, cfg.AdvertiseAddress)
		if err != nil {
			logger.Error("failed to auto-detect advertise HTTP base URL; set -advertise-http-base-url explicitly", "error", err)
			os.Exit(1)
		}
		cfg.AdvertiseHTTPBaseURL = resolvedURL
		logger.Info("auto-detected advertise HTTP base URL", "advertiseHTTPBaseURL", resolvedURL)
	}

	server, err := netboot.NewServer(cfg, logger)
	if err != nil {
		logger.Error("failed to create netboot server", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting netboot-server",
		"dhcpBindAddress", dhcpBindAddress,
		"tftpBindAddress", tftpBindAddress,
		"httpBindAddress", httpBindAddress,
		"advertiseHTTPBaseURL", cfg.AdvertiseHTTPBaseURL,
	)

	if err := server.Run(ctx); err != nil {
		logger.Error("netboot server exited with error", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// newResolverは、in-cluster/kubeconfigのいずれかからKubernetes APIへ接続し、
// TartHost/TartMachineをread-onlyで参照するnetboot.HostImageResolverを作成する。
// クラスタ外(ローカル検証など)で kubeconfig が見つからない場合はerrorを返し、呼び出し側で
// discovery imageのみのfallback動作へ切り替える。
func newResolver(logger *slog.Logger) (domainnetboot.HostImageResolver, error) {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	reader, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	logger.Info("connected to Kubernetes API for TartHost/TartMachine resolution")
	return httpboot.NewTartHostImageResolver(reader)
}
