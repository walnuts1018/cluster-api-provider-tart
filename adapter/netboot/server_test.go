package netboot

import (
	"log/slog"
	"testing"
)

func TestNewServerValidatesRequiredConfiguration(t *testing.T) {
	t.Parallel()

	base := Config{
		TFTPRoot:             t.TempDir(),
		DHCPBindAddress:      "127.0.0.1",
		TFTPBindAddress:      "127.0.0.1:1069",
		HTTPBindAddress:      "127.0.0.1:8080",
		AdvertiseHTTPBaseURL: "http://test.walnuts.dev:8080",
		AdvertiseAddress:     "192.0.2.10",
	}
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{name: "TFTP root", edit: func(config *Config) { config.TFTPRoot = "" }, want: "TFTPRoot is required"},
		{name: "DHCP bind address", edit: func(config *Config) { config.DHCPBindAddress = "" }, want: "DHCPBindAddress is required"},
		{name: "TFTP bind address", edit: func(config *Config) { config.TFTPBindAddress = "" }, want: "TFTPBindAddress is required"},
		{name: "HTTP bind address", edit: func(config *Config) { config.HTTPBindAddress = "" }, want: "HTTPBindAddress is required"},
		{name: "advertised HTTP URL", edit: func(config *Config) { config.AdvertiseHTTPBaseURL = "" }, want: "AdvertiseHTTPBaseURL is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := base
			tt.edit(&config)
			if _, err := NewServer(config, slog.New(slog.DiscardHandler)); err == nil || err.Error() != tt.want {
				t.Fatalf("NewServer() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNewServerBuildsAllProtocolServers(t *testing.T) {
	t.Parallel()

	server, err := NewServer(Config{
		TFTPRoot:             t.TempDir(),
		DHCPBindAddress:      "127.0.0.1",
		TFTPBindAddress:      "127.0.0.1:1069",
		HTTPBindAddress:      "127.0.0.1:8080",
		AdvertiseAddress:     "192.0.2.10",
		AdvertiseHTTPBaseURL: "http://test.walnuts.dev:8080",
	}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if server.dhcp == nil || server.tftp == nil || server.http == nil {
		t.Fatalf("NewServer() returned incomplete server: %#v", server)
	}
}
