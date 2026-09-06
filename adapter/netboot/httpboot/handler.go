// Package httpbootは、iPXEブートローダから呼び出されるiPXEスクリプト配信用のHTTP handlerと、
// Kubernetes APIをread-onlyで参照してPXEクライアントのMACアドレスからTartHost/TartMachineの
// desired Talos imageを解決するresolverの実装を提供する。
package httpboot

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
)

// ImageFactoryPXEBaseURLDefaultは、Talos Image FactoryがiPXEスクリプトを直接返すPXE配信endpointの既定baseURLである。
// "<base>/pxe/<schematicID>/v<version>/metal-<arch>"の形式でiPXEスクリプトが取得できる。
const ImageFactoryPXEBaseURLDefault = "https://pxe.factory.talos.dev"

// Handlerは、iPXEブートローダから呼び出されるiPXEスクリプト配信用のhttp.Handlerである。
type Handler struct {
	imageFactoryPXEBaseURL string
	discoveryImage         domainnetboot.DiscoveryImage
	resolver               domainnetboot.HostImageResolver
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
		imageFactoryPXEBaseURL: strings.TrimSuffix(imageFactoryPXEBaseURL, "/"),
		discoveryImage:         discoveryImage,
		resolver:               resolver,
		logger:                 logger.With("component", "httpboot"),
	}, nil
}

// RegisterはHandlerが提供するendpointをmuxへ登録する。
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/ipxe", h.handleIPXEScript)
	mux.HandleFunc("/healthz", h.handleHealthz)
}

// handleIPXEScriptは、iPXEブートローダへ返すiPXEスクリプトを生成する。
// Image FactoryのPXE配信endpointへchainするだけで、kernel/initramfsのURLはTart側で組み立てない。
// macに対応するTartHost/TartMachineが解決できた場合はそのdesired imageを、解決できない場合は
// discovery用のimageを配信する。
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

	chainURL := fmt.Sprintf("%s/pxe/%s/%s/metal-%s",
		h.imageFactoryPXEBaseURL, image.SchematicID, image.Version, arch)

	script := fmt.Sprintf("#!ipxe\necho Booting Talos (%s, %s, source=%s)\nchain %s\n",
		image.Version, image.SchematicID, source, chainURL)

	h.logger.Info("serving ipxe script", "mac", mac, "arch", arch, "chainURL", chainURL, "source", source)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(script)); err != nil {
		h.logger.Error("failed to write ipxe script response", "error", err)
	}
}

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
