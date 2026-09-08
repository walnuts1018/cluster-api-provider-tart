package httpboot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
)

type fakeHostImageResolver struct {
	image      domainnetboot.BootImage
	found      bool
	err        error
	calledWith string
	callCount  int
}

func (r *fakeHostImageResolver) ResolveBootImage(_ context.Context, mac string) (domainnetboot.BootImage, bool, error) {
	r.calledWith = mac
	r.callCount++
	return r.image, r.found, r.err
}

func TestHandlerIPXEScript(t *testing.T) {
	t.Parallel()

	const (
		discoveryVer = "v1.14.0"
		discoveryID  = "discovery-schematic"
		resolvedVer  = "v1.15.0"
		resolvedID   = "resolved-schematic"
		requestMAC   = "00:00:5e:00:53:02"
	)

	// Talos Image Factoryを模したserver。/pxe/<schematic>/<version>/metal-<arch>へのrequestに
	// 対し、httpsのkernel/initrd URLを埋め込んだscriptを返す(実際のFactoryの応答形式を模する)。
	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pxe/missing-schematic/v1.14.0/metal-amd64" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "#!ipxe\n\nimgfree\nkernel https://factory.test.walnuts.dev/image/%s/kernel talos.platform=metal\ninitrd https://factory.test.walnuts.dev/image/%s/initrd\nboot\n",
			r.URL.Path, r.URL.Path)
	}))
	t.Cleanup(factory.Close)
	baseURL := factory.URL

	tests := map[string]struct {
		requestURL     string
		discoveryImage domainnetboot.DiscoveryImage
		resolvedImage  domainnetboot.BootImage
		resolvedFound  bool
		resolverErr    error
		wantStatus     int
		wantBodyPrefix string
		wantMAC        string
		wantCallCount  int
	}{
		"discovery": {
			requestURL:     "/ipxe?arch=arm64",
			discoveryImage: domainnetboot.DiscoveryImage{Version: discoveryVer, SchematicID: discoveryID},
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "#!ipxe\necho Booting Talos (v1.14.0, discovery-schematic, source=discovery)\n\nimgfree\nkernel http://",
		},
		"resolved": {
			requestURL:     "/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02",
			discoveryImage: domainnetboot.DiscoveryImage{Version: discoveryVer, SchematicID: discoveryID},
			resolvedImage:  domainnetboot.BootImage{Version: resolvedVer, SchematicID: resolvedID},
			resolvedFound:  true,
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "#!ipxe\necho Booting Talos (v1.15.0, resolved-schematic, source=resolved)\n\nimgfree\nkernel http://",
			wantMAC:        requestMAC,
			wantCallCount:  1,
		},
		"not found falls back": {
			requestURL:     "/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02",
			discoveryImage: domainnetboot.DiscoveryImage{Version: discoveryVer, SchematicID: discoveryID},
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "#!ipxe\necho Booting Talos (v1.14.0, discovery-schematic, source=discovery)\n\nimgfree\nkernel http://",
			wantMAC:        requestMAC,
			wantCallCount:  1,
		},
		"resolver error falls back": {
			requestURL:     "/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02",
			discoveryImage: domainnetboot.DiscoveryImage{Version: discoveryVer, SchematicID: discoveryID},
			resolverErr:    errors.New("API unavailable"),
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "#!ipxe\necho Booting Talos (v1.14.0, discovery-schematic, source=discovery)\n\nimgfree\nkernel http://",
			wantMAC:        requestMAC,
			wantCallCount:  1,
		},
		"factory fetch failure": {
			requestURL:     "/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02",
			discoveryImage: domainnetboot.DiscoveryImage{Version: discoveryVer, SchematicID: "missing-schematic"},
			wantStatus:     http.StatusBadGateway,
			wantBodyPrefix: "#!ipxe\necho Failed to fetch the Talos boot script from the Image Factory:",
			wantMAC:        requestMAC,
			wantCallCount:  1,
		},
		"zero image": {
			requestURL:     "/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02",
			wantStatus:     http.StatusServiceUnavailable,
			wantBodyPrefix: "#!ipxe\necho No Talos boot image is configured for this host yet.\necho Register a TartHost for this MAC address, or set the netboot-server discovery image, and retry.\nreboot\n",
			wantMAC:        requestMAC,
			wantCallCount:  1,
		},
		"resolved zero image": {
			requestURL:     "/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02",
			resolvedFound:  true,
			wantStatus:     http.StatusServiceUnavailable,
			wantBodyPrefix: "#!ipxe\necho No Talos boot image is configured for this host yet.\necho Register a TartHost for this MAC address, or set the netboot-server discovery image, and retry.\nreboot\n",
			wantMAC:        requestMAC,
			wantCallCount:  1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resolver := &fakeHostImageResolver{image: tt.resolvedImage, found: tt.resolvedFound, err: tt.resolverErr}
			handler, err := NewHandler(baseURL, tt.discoveryImage, resolver, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}

			mux := http.NewServeMux()
			handler.Register(mux)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.requestURL, nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if !strings.HasPrefix(response.Body.String(), tt.wantBodyPrefix) {
				t.Errorf("body = %q, want prefix %q", response.Body.String(), tt.wantBodyPrefix)
			}
			if strings.Contains(response.Body.String(), "https://") {
				t.Errorf("body still contains an https:// URL: %q", response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
			}
			if resolver.calledWith != tt.wantMAC || resolver.callCount != tt.wantCallCount {
				t.Errorf("resolver call = (%q, %d), want (%q, %d)", resolver.calledWith, resolver.callCount, tt.wantMAC, tt.wantCallCount)
			}
		})
	}
}

func TestNewHandlerValidatesInputsAndNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	resolver := &fakeHostImageResolver{}
	tests := map[string]struct {
		baseURL     string
		discovery   domainnetboot.DiscoveryImage
		resolver    domainnetboot.HostImageResolver
		wantBaseURL string
		wantErr     bool
	}{
		"default base URL": {
			resolver:    resolver,
			wantBaseURL: ImageFactoryPXEBaseURLDefault,
		},
		"trailing slash": {
			baseURL:     "https://factory.test.walnuts.dev///",
			resolver:    resolver,
			wantBaseURL: "https://factory.test.walnuts.dev",
		},
		"invalid discovery version": {
			discovery: domainnetboot.DiscoveryImage{Version: "1.14.0", SchematicID: "schematic-a"},
			resolver:  resolver,
			wantErr:   true,
		},
		"nil resolver": {wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewHandler(tt.baseURL, tt.discovery, tt.resolver, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewHandler() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err == nil && handler.imageFactoryPXEBaseURL != tt.wantBaseURL {
				t.Errorf("imageFactoryPXEBaseURL = %q, want %q", handler.imageFactoryPXEBaseURL, tt.wantBaseURL)
			}
		})
	}
}
