// Package netbootは、まっさらな実機がPXE bootでTalos maintenance modeへ到達するために必要な
// ProxyDHCP、TFTP、iPXEスクリプト配信をcontroller-managerとは独立したアダプターとして提供する。
// netboot-serverはKubernetes APIをread-onlyで参照し、PXEクライアントのMACアドレスから
// TartHost/TartMachineのdesired Talos image(spec.image)を解決してPXEクライアントを
// Talos Image Factoryへ橋渡しする。対応するTartHost/TartMachineがまだ存在しない場合
// (Host登録前の初回enrollment boot)は、operatorが指定したdiscovery用のTalos
// version/schematicIDへfallbackする。Secretやmachine configurationはnetboot-serverの
// スコープ外であり、maintenance mode起動後のconfiguration適用はcontroller-manager側が扱う。
package netboot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot/dhcp"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot/httpboot"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot/tftp"
	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
)

// Configはnetboot-serverの起動設定である。
type Config struct {
	// TFTPRootはiPXEブートローダを配置するディレクトリである。
	TFTPRoot string
	// DHCPBindAddressはProxyDHCPのbind addressである(通常はhostのIP、ポートは内部で67/4011を使う)。
	DHCPBindAddress string
	// TFTPBindAddressはTFTPサーバーのbind address(host:port)である。
	TFTPBindAddress string
	// HTTPBindAddressはiPXEスクリプト配信用HTTPサーバーのbind address(host:port)である。
	HTTPBindAddress string
	// AdvertiseAddressは、PXEクライアントに広告する到達可能なサーバーIPである。空の場合は自動検出を試みる。
	AdvertiseAddress string
	// AdvertiseHTTPBaseURLは、iPXEスクリプトへ埋め込むHTTPサーバーのベースURL(例: http://192.0.2.10:8080)である。
	AdvertiseHTTPBaseURL string
	// ImageFactoryPXEBaseURLはTalos Image FactoryのPXE配信endpointのbaseURLである。空の場合はhttpboot.ImageFactoryPXEBaseURLDefaultを使う。
	ImageFactoryPXEBaseURL string
	// DiscoveryImageは素のhostをTalos maintenance modeへ到達させるためのdiscovery用Talos imageである。
	// TartHost/TartMachineがまだ存在しないMACアドレスからのPXEリクエストに対するfallbackとして使う。
	DiscoveryImage domainnetboot.DiscoveryImage
	// Resolverは、PXEクライアントのMACアドレスからTartHost/TartMachineのdesired imageを
	// read-onlyで解決する。nilの場合は常にDiscoveryImageのみを使う。
	Resolver domainnetboot.HostImageResolver
}

// Serverは、ProxyDHCP、TFTP、iPXEスクリプト配信用HTTPサーバーをまとめて起動するnetboot-serverの本体である。
type Server struct {
	cfg    Config
	logger *slog.Logger

	dhcp *dhcp.Server
	tftp *tftp.Server
	http *http.Server
}

// NewServerは設定を検証し、新しいServerを作成する。
func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	if cfg.TFTPRoot == "" {
		return nil, errors.New("TFTPRoot is required")
	}
	if cfg.DHCPBindAddress == "" {
		return nil, errors.New("DHCPBindAddress is required")
	}
	if cfg.TFTPBindAddress == "" {
		return nil, errors.New("TFTPBindAddress is required")
	}
	if cfg.HTTPBindAddress == "" {
		return nil, errors.New("HTTPBindAddress is required")
	}
	if cfg.AdvertiseHTTPBaseURL == "" {
		return nil, errors.New("AdvertiseHTTPBaseURL is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	advertiseIP, err := dhcp.ResolveAdvertiseIP(cfg.DHCPBindAddress, cfg.HTTPBindAddress, cfg.AdvertiseAddress)
	if err != nil {
		return nil, fmt.Errorf("resolve advertise address: %w", err)
	}

	dhcpServer, err := dhcp.NewServer(cfg.TFTPRoot, cfg.DHCPBindAddress, advertiseIP.String(), cfg.AdvertiseHTTPBaseURL, logger)
	if err != nil {
		return nil, fmt.Errorf("create DHCP server: %w", err)
	}

	tftpServer, err := tftp.NewServer(cfg.TFTPRoot, cfg.TFTPBindAddress, logger)
	if err != nil {
		return nil, fmt.Errorf("create TFTP server: %w", err)
	}

	resolver := cfg.Resolver
	if resolver == nil {
		resolver = domainnetboot.NoopHostImageResolver{}
	}
	bootHandler, err := httpboot.NewHandler(cfg.ImageFactoryPXEBaseURL, cfg.DiscoveryImage, resolver, logger)
	if err != nil {
		return nil, fmt.Errorf("create HTTP boot handler: %w", err)
	}
	mux := http.NewServeMux()
	bootHandler.Register(mux)

	return &Server{
		cfg:    cfg,
		logger: logger,
		dhcp:   dhcpServer,
		tftp:   tftpServer,
		http: &http.Server{
			Addr:              cfg.HTTPBindAddress,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

// Runはctxがキャンセルされる、またはいずれかのサーバーが致命的なerrorで終了するまでブロックする。
// DHCP、TFTP、HTTPの3サーバーを並行して起動し、いずれかが停止したら他のサーバーも停止する。
func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 3)
	var wg sync.WaitGroup

	wg.Go(func() {
		if err := s.dhcp.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("dhcp server: %w", err)
			cancel()
			return
		}
		errCh <- nil
	})

	wg.Go(func() {
		if err := s.tftp.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("tftp server: %w", err)
			cancel()
			return
		}
		errCh <- nil
	})

	wg.Go(func() {
		go func() {
			<-ctx.Done()
			if err := s.http.Shutdown(context.Background()); err != nil { //nolint:contextcheck // ctxは既にDoneのため、shutdown処理には新しいcontextを使う必要がある
				s.logger.Error("failed to shut down HTTP boot server", "error", err)
			}
		}()
		s.logger.Info("starting HTTP boot server", "address", s.cfg.HTTPBindAddress)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
			cancel()
			return
		}
		errCh <- nil
	})

	wg.Wait()
	close(errCh)

	var firstErr error
	for err := range errCh {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
