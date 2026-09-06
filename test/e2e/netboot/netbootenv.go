//go:build e2e

// Package netbootは、Tart自前のcmd/netboot-serverが使うadapter/netbootを、E2Eテストプロセス
// 内から直接起動するための配線を提供する。cmd/netboot-server自体をsubprocessとして
// 起動する代わりにこのpackageからadapter/netboot.NewServerを直接呼び出すことで、
// テストプロセスと同じcontextでの停止・エラー伝播・ログ収集を単純にする。
package netboot

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot/dhcp"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/netboot/httpboot"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
	"github.com/walnuts1018/cluster-api-provider-tart/utils/logger"
)

// newSchemeは、netboot resolverがTartHost/TartMachineをdecodeするために必要な最小schemeを作る。
func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		panic(fmt.Errorf("register infrastructure scheme: %w", err))
	}
	return scheme
}

// Configはテストプロセス内で起動するnetboot-serverの設定である。
type Config struct {
	// KubeconfigPathはkind cluster外(labのVMと同一network上で動くこのテストプロセス)から
	// kind clusterのKubernetes APIへ到達するためのkubeconfig pathである。
	KubeconfigPath string
	// TFTPRoot/DHCPBindAddress等はadapter/netboot.Configと同じ意味を持つ。
	TFTPRoot               string
	DHCPBindAddress        string
	TFTPBindAddress        string
	HTTPBindAddress        string
	AdvertiseAddress       string
	AdvertiseHTTPBaseURL   string
	ImageFactoryPXEBaseURL string
	DiscoveryTalosVersion  string
	DiscoverySchematicID   string
}

// Serverは起動済みのnetboot-serverと、それをstopするための機構を保持する。
type Server struct {
	inner  *netboot.Server
	cancel context.CancelFunc
	done   chan error
}

// Startは、kind clusterのkubeconfigからTartHost/TartMachineをread-onlyに参照するclientを作り、
// cmd/netboot-serverと同じ配線(httpboot.NewTartHostImageResolver)でnetboot-serverを起動する。
// PXE要求はVM(lab)と同じL2 segment上のHost(このテストプロセスを動かすCI runner)から
// 直接応答できる必要があるため、bind addressはlab networkに到達可能なinterfaceへ向ける。
func Start(ctx context.Context, cfg Config) (*Server, error) {
	restConfig, err := clientcmd.BuildConfigFromFlags("", cfg.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig from %q: %w", cfg.KubeconfigPath, err)
	}

	reader, err := client.New(restConfig, client.Options{Scheme: newScheme()})
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client for netboot resolver: %w", err)
	}

	resolver, err := httpboot.NewTartHostImageResolver(reader)
	if err != nil {
		return nil, fmt.Errorf("create TartHost image resolver: %w", err)
	}

	// AdvertiseHTTPBaseURLが未設定の場合、cmd/netboot-server/main.goと同じ規則で自動解決する
	// (adapter/netboot.NewServer自体は明示値を必須とし、自動解決ロジックを持たない)。
	advertiseHTTPBaseURL := cfg.AdvertiseHTTPBaseURL
	if advertiseHTTPBaseURL == "" {
		resolvedURL, resolveErr := dhcp.DefaultAdvertiseHTTPBaseURL(cfg.DHCPBindAddress, cfg.HTTPBindAddress, cfg.AdvertiseAddress)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve default advertise HTTP base URL: %w", resolveErr)
		}
		advertiseHTTPBaseURL = resolvedURL
	}

	serverConfig := netboot.Config{
		TFTPRoot:               cfg.TFTPRoot,
		DHCPBindAddress:        cfg.DHCPBindAddress,
		TFTPBindAddress:        cfg.TFTPBindAddress,
		HTTPBindAddress:        cfg.HTTPBindAddress,
		AdvertiseAddress:       cfg.AdvertiseAddress,
		AdvertiseHTTPBaseURL:   advertiseHTTPBaseURL,
		ImageFactoryPXEBaseURL: cfg.ImageFactoryPXEBaseURL,
		DiscoveryImage: domainnetboot.DiscoveryImage{
			Version:     cfg.DiscoveryTalosVersion,
			SchematicID: cfg.DiscoverySchematicID,
		},
		Resolver: resolver,
	}

	logHandler := logger.Create("info", "text")
	server, err := netboot.NewServer(serverConfig, logHandler)
	if err != nil {
		return nil, fmt.Errorf("create netboot server: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- server.Run(runCtx)
	}()

	// 起動直後にDHCP/TFTP/HTTPのbindが失敗していないか短時間だけ確認する。
	select {
	case err := <-done:
		cancel()
		return nil, fmt.Errorf("netboot server exited immediately: %w", err)
	case <-time.After(500 * time.Millisecond):
	}

	return &Server{inner: server, cancel: cancel, done: done}, nil
}

// Stopはnetboot-serverを停止し、終了を待つ。
func (s *Server) Stop(ctx context.Context) error {
	s.cancel()
	select {
	case err := <-s.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
