// Package netbootは、まっさらな実機がPXE bootでTalos maintenance modeへ到達するために必要な
// ProxyDHCP、TFTP、iPXEスクリプト配信をcontroller-managerとは独立したアダプターとして提供する。
// netboot-serverはKubernetes APIをread-onlyで参照し、PXEクライアントのMACアドレスから
// TartHost/TartMachineのdesired Talos image(spec.image)を解決してPXEクライアントを
// Talos Image Factoryへ橋渡しする。対応するTartHost/TartMachineがまだ存在しない場合
// (Host登録前の初回enrollment boot)は、operatorが指定したdiscovery用のTalos
// version/schematicIDへfallbackする。Secretや machine configurationはnetboot-serverの
// スコープ外であり、maintenance mode起動後のconfiguration適用はcontroller-manager側が扱う。
package netboot

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// ImageFactoryPXEBaseURLDefaultは、Talos Image FactoryがiPXEスクリプトを直接返すPXE配信endpointの既定baseURLである。
// "<base>/pxe/<schematicID>/v<version>/metal-<arch>"の形式でiPXEスクリプトが取得できる。
const ImageFactoryPXEBaseURLDefault = "https://pxe.factory.talos.dev"

// DiscoveryImageはPXE bootした素のhostをTalos maintenance modeへ到達させるためのdesired Talos imageである。
// TartMachine個別のinstaller image(spec.talosImageの{version, schematicID})とは別物であり、
// operatorがnetboot-serverの設定として指定するdiscovery専用の値を保持する。
// このimageはTartHost/TartMachineへclaimされる前の初回enrollment bootでのみ使われるため、
// 未設定のままでもnetboot-server自体は起動し、既にTartHost/TartMachineへclaimされたHostの
// PXE bootは通常通り解決できる。未設定のまま初回enrollment bootを行うhostへは、
// discovery先が不明であることを示すiPXEスクリプトを返す。
type DiscoveryImage struct {
	// Versionは"v"始まりのTalos version(例: v1.11.2)である。空の場合は未設定として扱う。
	Version string
	// SchematicIDはTalos Image Factoryのschematic identifierである。空の場合は未設定として扱う。
	SchematicID string
}

// IsZeroはDiscoveryImageが未設定かを返す。
func (image DiscoveryImage) IsZero() bool {
	return strings.TrimSpace(image.Version) == "" || strings.TrimSpace(image.SchematicID) == ""
}

// HTTPBootHandlerは、iPXEブートローダから呼び出されるiPXEスクリプト配信用のhttp.Handlerである。
type HTTPBootHandler struct {
	imageFactoryPXEBaseURL string
	discoveryImage         DiscoveryImage
	resolver               HostImageResolver
	logger                 *slog.Logger
}

// NewHTTPBootHandlerは新しいHTTPBootHandlerを作成する。
// imageFactoryPXEBaseURLが空の場合はImageFactoryPXEBaseURLDefaultを使用する。
// resolverはPXEクライアントのMACアドレスからTartHost/TartMachineのdesired imageを解決する。
// 対応するTartHost/TartMachineが見つからない場合はdiscoveryImageへfallbackするため、resolver自体は必須だが
// resolverが未検出(found=false)を返すことは正常なケースである。
func NewHTTPBootHandler(imageFactoryPXEBaseURL string, discoveryImage DiscoveryImage, resolver HostImageResolver, logger *slog.Logger) (*HTTPBootHandler, error) {
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

	return &HTTPBootHandler{
		imageFactoryPXEBaseURL: strings.TrimSuffix(imageFactoryPXEBaseURL, "/"),
		discoveryImage:         discoveryImage,
		resolver:               resolver,
		logger:                 logger.With("component", "httpboot"),
	}, nil
}

// RegisterはHTTPBootHandlerが提供するendpointをmuxへ登録する。
func (h *HTTPBootHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/ipxe", h.handleIPXEScript)
	mux.HandleFunc("/healthz", h.handleHealthz)
}

// handleIPXEScriptは、iPXEブートローダへ返すiPXEスクリプトを生成する。
// Image FactoryのPXE配信endpointへchainするだけで、kernel/initramfsのURLはTart側で組み立てない。
// macに対応するTartHost/TartMachineが解決できた場合はそのdesired imageを、解決できない場合は
// discovery用のimageを配信する。
func (h *HTTPBootHandler) handleIPXEScript(w http.ResponseWriter, r *http.Request) {
	mac := r.URL.Query().Get("mac")
	arch := pxeArchFromQuery(r.URL.Query().Get("arch"))

	image := h.discoveryImage
	source := "discovery"
	if mac != "" {
		if resolved, found, err := h.resolver.ResolveBootImage(r.Context(), mac); err != nil {
			h.logger.Error("failed to resolve boot image, falling back to discovery image", "mac", mac, "error", err)
		} else if found {
			image = DiscoveryImage{Version: resolved.Version, SchematicID: resolved.SchematicID}
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

func (h *HTTPBootHandler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// pxeArchFromQueryは、クエリパラメータからImage FactoryのPXE配信endpointが要求するarch文字列を決定する。
// 現時点でDHCPServerはamd64のみをiPXEブートローダへ誘導するため既定値はamd64だが、
// 将来arm64クライアントにも対応できるようクエリで上書きできるようにしておく。
func pxeArchFromQuery(arch string) string {
	switch strings.ToLower(arch) {
	case "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}
