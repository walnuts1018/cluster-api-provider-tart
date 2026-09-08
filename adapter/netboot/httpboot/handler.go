// Package httpbootは、iPXEブートローダから呼び出されるiPXEスクリプト配信用のHTTP handlerと、
// Kubernetes APIをread-onlyで参照してPXEクライアントのMACアドレスからTartHost/TartMachineの
// desired Talos imageを解決するresolverの実装を提供する。
package httpboot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
)

// factoryFetchTimeoutは、Talos Image FactoryのPXEスクリプトをサーバーサイドで取得する際の
// タイムアウトである。iPXEクライアント自身のTFTP/HTTP requestタイムアウトより短く保ち、
// Factoryへの到達性問題をこちら側のtimeoutとして早めに切り上げる。
const factoryFetchTimeout = 15 * time.Second

// ImageFactoryPXEBaseURLDefaultは、Talos Image FactoryがiPXEスクリプトを直接返すPXE配信endpointの既定baseURLである。
// "<base>/pxe/<schematicID>/v<version>/metal-<arch>"の形式でiPXEスクリプトが取得できる。
const ImageFactoryPXEBaseURLDefault = "https://pxe.factory.talos.dev"

// Handlerは、iPXEブートローダから呼び出されるiPXEスクリプト配信用のhttp.Handlerである。
type Handler struct {
	imageFactoryPXEBaseURL string
	discoveryImage         domainnetboot.DiscoveryImage
	resolver               domainnetboot.HostImageResolver
	httpClient             *http.Client
	logger                 *slog.Logger
}

// NewHandlerは新しいHandlerを作成する。
// imageFactoryPXEBaseURLが空の場合はImageFactoryPXEBaseURLDefaultを使用する。
// resolverはPXEクライアントのMACアドレスからTartHost/TartMachineのdesired imageを解決する。
// 対応するTartHost/TartMachineが見つからない場合はdiscoveryImageへfallbackするため、resolver自体は必須だが
// resolverが未検出(found=false)を返すことは正常なケースである。
func NewHandler(imageFactoryPXEBaseURL string, discoveryImage domainnetboot.DiscoveryImage, resolver domainnetboot.HostImageResolver, logger *slog.Logger) (*Handler, error) {
	if !discoveryImage.IsZero() && !strings.HasPrefix(discoveryImage.Version, "v") {
		return nil, errors.New("discovery image version must start with v")
	}
	if resolver == nil {
		return nil, errors.New("resolver is required")
	}
	if imageFactoryPXEBaseURL == "" {
		imageFactoryPXEBaseURL = ImageFactoryPXEBaseURLDefault
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		imageFactoryPXEBaseURL: strings.TrimRight(imageFactoryPXEBaseURL, "/"),
		discoveryImage:         discoveryImage,
		resolver:               resolver,
		httpClient:             &http.Client{Timeout: factoryFetchTimeout},
		logger:                 logger.With("component", "httpboot"),
	}, nil
}

// RegisterはHandlerが提供するendpointをmuxへ登録する。
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/ipxe", h.handleIPXEScript)
	mux.HandleFunc("/healthz", h.handleHealthz)
}

// handleIPXEScriptは、iPXEブートローダへ返すiPXEスクリプトを生成する。
// kernel/initramfsのURLはTart側で組み立てず、Talos Image FactoryのPXE配信endpointが返す
// scriptをそのまま使うが、埋め込まれたhttps:// URLはhttp://へ書き換えて配信する
// (fetchAndDowngradeScriptのコメント参照)。macに対応するTartHost/TartMachineが解決できた
// 場合はそのdesired imageを、解決できない場合はdiscovery用のimageを配信する。
func (h *Handler) handleIPXEScript(w http.ResponseWriter, r *http.Request) {
	mac := r.URL.Query().Get("mac")
	arch := domainnetboot.PXEArchFromQuery(r.URL.Query().Get("arch"))

	image := h.discoveryImage
	source := "discovery"
	if mac != "" {
		if resolved, found, err := h.resolver.ResolveBootImage(r.Context(), mac); err != nil {
			h.logger.Error("failed to resolve boot image, falling back to discovery image", "mac", mac, "error", err)
		} else if found {
			image = domainnetboot.DiscoveryImage(resolved)
			source = "resolved"
		}
	}

	if image.IsZero() {
		h.logger.Warn("no boot image is available for this PXE request; the discovery image is not configured and no TartHost/TartMachine matched this MAC", "mac", mac)
		script := "#!ipxe\necho No Talos boot image is configured for this host yet.\necho Register a TartHost for this MAC address, or set the netboot-server discovery image, and retry.\nreboot\n"
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte(script)); err != nil {
			h.logger.Error("failed to write ipxe script response", "error", err)
		}
		return
	}

	factoryURL := fmt.Sprintf("%s/pxe/%s/%s/metal-%s",
		h.imageFactoryPXEBaseURL, image.SchematicID, image.Version, arch)

	factoryScript, err := h.fetchAndDowngradeScript(r.Context(), factoryURL)
	if err == nil {
		factoryScript, err = insertEchoAfterShebang(factoryScript,
			fmt.Sprintf("echo Booting Talos (%s, %s, source=%s)", image.Version, image.SchematicID, source))
	}
	script := factoryScript
	if err != nil {
		// legacy BIOS PXE(undionly.kpxe)はHTTPSに対応していないため、Talos Image FactoryのPXE
		// script自体はこちらでサーバーサイド取得しhttps://をhttp://へ書き換えて配信する
		// (chainで直接https URLへ飛ばすと、undionly.kpxeが"Operation not supported"で失敗する)。
		// FactoryへのHTTPS到達性問題そのものはこちらのfetch自体も失敗させるため、その場合は
		// クライアントへエラーを説明するscriptへfallbackする。
		h.logger.Error("failed to fetch Talos Image Factory PXE script", "mac", mac, "arch", arch, "factoryURL", factoryURL, "error", err)
		script = fmt.Sprintf("#!ipxe\necho Failed to fetch the Talos boot script from the Image Factory: %s\nreboot\n", err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		if _, err := w.Write([]byte(script)); err != nil {
			h.logger.Error("failed to write ipxe script response", "error", err)
		}
		return
	}

	h.logger.Info("serving ipxe script", "mac", mac, "arch", arch, "factoryURL", factoryURL, "source", source)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(script)); err != nil {
		h.logger.Error("failed to write ipxe script response", "error", err)
	}
}

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// fetchAndDowngradeScriptは、Talos Image FactoryのPXE配信endpointからiPXE scriptを取得し、
// script内に埋め込まれたhttps:// URLをhttp://へ書き換えて返す。legacy BIOS PXE(undionly.kpxe)
// はTLSに対応しておらず、埋め込まれたkernel/initrdのhttps URLを直接chainするとTLS handshake
// できずに失敗する。Factoryは同じcontentをplain HTTPでも配信しているため、この書き換えで
// legacy BIOS/UEFIどちらのクライアントでも同じscriptで起動できる。
func (h *Handler) fetchAndDowngradeScript(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.logger.Error("failed to close Image Factory response body", "error", err)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body from %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return strings.ReplaceAll(string(body), "https://", "http://"), nil
}

// insertEchoAfterShebangは、iPXE scriptの先頭行(#!ipxe)の直後へecho行を挿入する。
// scriptが#!ipxeで始まっていない場合はFactoryの応答形式が想定と異なるとみなしエラーを返す。
func insertEchoAfterShebang(script, echoLine string) (string, error) {
	const shebang = "#!ipxe"
	rest, ok := strings.CutPrefix(script, shebang+"\n")
	if !ok {
		return "", fmt.Errorf("unexpected Image Factory script format (missing %q header)", shebang)
	}
	return shebang + "\n" + echoLine + "\n" + rest, nil
}
