package dhcp

import (
	"net"
	"testing"
)

func TestResolveAdvertiseIP(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		bindAddr      string
		httpAddr      string
		advertiseAddr string
		want          string
		wantErr       bool
	}{
		"明示設定を優先": {
			bindAddr: "192.0.2.11:67", httpAddr: "192.0.2.12:8080", advertiseAddr: "192.0.2.10", want: "192.0.2.10",
		},
		"bind addressから解決": {
			bindAddr: "192.0.2.11:67", httpAddr: ":8080", want: "192.0.2.11",
		},
		"HTTP addressへfallback": {
			bindAddr: ":67", httpAddr: "192.0.2.12:8080", want: "192.0.2.12",
		},
		"未指定bindとHTTPでも明示広告を使う": {
			bindAddr: "0.0.0.0:67", httpAddr: "0.0.0.0:8080", advertiseAddr: "192.0.2.10", want: "192.0.2.10",
		},
		"未指定の明示値はfallback": {
			bindAddr: "192.0.2.11", advertiseAddr: "0.0.0.0", want: "192.0.2.11",
		},
		"不正な明示値はerror": {
			bindAddr: "192.0.2.11:67", advertiseAddr: "netboot.test.walnuts.dev", wantErr: true,
		},
		"IPv6の明示設定": {
			bindAddr: ":67", httpAddr: ":8080", advertiseAddr: "2001:db8::10", want: "2001:db8::10",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveAdvertiseIP(tt.bindAddr, tt.httpAddr, tt.advertiseAddr)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolveAdvertiseIP() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveAdvertiseIP() error = %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("ResolveAdvertiseIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultAdvertiseHTTPBaseURL(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		dhcpBind      string
		httpBind      string
		advertiseAddr string
		want          string
		wantErr       bool
	}{
		"IPv4": {
			dhcpBind: "192.0.2.10:67", httpBind: ":8080", want: "http://192.0.2.10:8080",
		},
		"明示広告IPv4": {
			dhcpBind: ":67", httpBind: "192.0.2.11:18080", advertiseAddr: "192.0.2.12", want: "http://192.0.2.12:18080",
		},
		"IPv6はhostを括弧で囲む": {
			dhcpBind: ":67", httpBind: ":8080", advertiseAddr: "2001:db8::10", want: "http://[2001:db8::10]:8080",
		},
		"portなし": {
			dhcpBind: "192.0.2.10:67", httpBind: "8080", wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := DefaultAdvertiseHTTPBaseURL(tt.dhcpBind, tt.httpBind, tt.advertiseAddr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DefaultAdvertiseHTTPBaseURL() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("DefaultAdvertiseHTTPBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseHostIP(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"hostとport":    "192.0.2.10:8080",
		"hostのみ":       "192.0.2.11",
		"括弧付きIPv6":     "[2001:db8::10]:8080",
		"portなしIPv6":   "2001:db8::11",
		"hostnameは対象外": "netboot.test.walnuts.dev:8080",
		"空文字列":         "",
		"不正なIP":        "192.0.2.999:8080",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := ParseHostIP(input)
			if (name == "hostnameは対象外" || name == "空文字列" || name == "不正なIP") && got != nil {
				t.Errorf("ParseHostIP(%q) = %q, want nil", input, got)
			}
			if name == "hostとport" && !got.Equal(net.ParseIP("192.0.2.10")) {
				t.Errorf("ParseHostIP(%q) = %q, want 192.0.2.10", input, got)
			}
			if name == "hostのみ" && !got.Equal(net.ParseIP("192.0.2.11")) {
				t.Errorf("ParseHostIP(%q) = %q, want 192.0.2.11", input, got)
			}
			if name == "括弧付きIPv6" && !got.Equal(net.ParseIP("2001:db8::10")) {
				t.Errorf("ParseHostIP(%q) = %q, want 2001:db8::10", input, got)
			}
			if name == "portなしIPv6" && !got.Equal(net.ParseIP("2001:db8::11")) {
				t.Errorf("ParseHostIP(%q) = %q, want 2001:db8::11", input, got)
			}
		})
	}
}
