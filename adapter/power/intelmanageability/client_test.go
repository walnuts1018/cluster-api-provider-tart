package intelmanageability

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/device-management-toolkit/go-wsman-messages/v2/pkg/amterror"
	wsmanpower "github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman/cim/power"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
)

func TestParseEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		address            string
		wantDialAddress    string
		wantHostname       string
		wantTLS            bool
		wantErr            bool
		wantInvalidAddress bool
	}{
		{name: "http既定port/path省略", address: "http://amt.test.walnuts.dev", wantDialAddress: "amt.test.walnuts.dev:16992", wantHostname: "amt.test.walnuts.dev", wantTLS: false},
		{name: "http既定port明示", address: "http://amt.test.walnuts.dev:16992", wantDialAddress: "amt.test.walnuts.dev:16992", wantHostname: "amt.test.walnuts.dev", wantTLS: false},
		{name: "http既定path明示", address: "http://amt.test.walnuts.dev/wsman", wantDialAddress: "amt.test.walnuts.dev:16992", wantHostname: "amt.test.walnuts.dev", wantTLS: false},
		{name: "https既定port/path省略", address: "https://amt.test.walnuts.dev", wantDialAddress: "amt.test.walnuts.dev:16993", wantHostname: "amt.test.walnuts.dev", wantTLS: true},
		{name: "https既定port明示", address: "https://amt.test.walnuts.dev:16993", wantDialAddress: "amt.test.walnuts.dev:16993", wantHostname: "amt.test.walnuts.dev", wantTLS: true},
		// NAT/port forwarding越しに実機へ到達する構成を想定し、非既定portは受理する。
		{name: "httpの非標準portは許容する", address: "http://amt.test.walnuts.dev:8080", wantDialAddress: "amt.test.walnuts.dev:8080", wantHostname: "amt.test.walnuts.dev", wantTLS: false},
		{name: "httpsの非標準portは許容する", address: "https://amt.test.walnuts.dev:8443", wantDialAddress: "amt.test.walnuts.dev:8443", wantHostname: "amt.test.walnuts.dev", wantTLS: true},
		// Intel ME 7.1実機はWS-Man endpointを常に"/wsman"直下にのみ公開するため、それ以外のpathは拒否する。
		{name: "非標準pathは拒否", address: "http://amt.test.walnuts.dev/amt", wantErr: true},
		{name: "queryは拒否", address: "http://amt.test.walnuts.dev/wsman?next=admin", wantErr: true, wantInvalidAddress: true},
		{name: "fragmentは拒否", address: "http://amt.test.walnuts.dev/wsman#top", wantErr: true, wantInvalidAddress: true},
		{name: "schemeなしは拒否", address: "amt.test.walnuts.dev", wantErr: true, wantInvalidAddress: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// scheme/query/fragmentの構文検証はparseEndpoint呼び出し前のendpoint.ParseHTTPURLが担う。
			address, parseErr := endpoint.ParseHTTPURL(test.address)
			if test.wantInvalidAddress {
				if !errors.Is(parseErr, endpoint.ErrInvalidHTTPURL) {
					t.Fatalf("ParseHTTPURL(%q) error = %v, want ErrInvalidHTTPURL", test.address, parseErr)
				}
				return
			}
			if parseErr != nil {
				t.Fatalf("ParseHTTPURL(%q) error = %v", test.address, parseErr)
			}
			dialAddress, hostname, useTLS, err := parseEndpoint(address)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseEndpoint(%q) error = %v, wantErr = %t", test.address, err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if dialAddress != test.wantDialAddress || hostname != test.wantHostname || useTLS != test.wantTLS {
				t.Fatalf("parseEndpoint(%q) = (%q, %q, %t), want (%q, %q, %t)", test.address, dialAddress, hostname, useTLS, test.wantDialAddress, test.wantHostname, test.wantTLS)
			}
		})
	}
}

// mustHTTPURLはtestで既知の有効なaddress文字列をendpoint.HTTPURLへ変換する。
func mustHTTPURL(t *testing.T, value string) endpoint.HTTPURL {
	t.Helper()
	parsed, err := endpoint.ParseHTTPURL(value)
	if err != nil {
		t.Fatalf("ParseHTTPURL(%q) error = %v", value, err)
	}
	return parsed
}

func TestNewClientRejectsMissingCredential(t *testing.T) {
	t.Parallel()

	address := mustHTTPURL(t, "http://amt.test.walnuts.dev")
	if _, err := newClient(Config{Address: address, Username: "", Password: "secret"}); err == nil {
		t.Fatal("newClient() error = nil, want validation error for missing username")
	}
	if _, err := newClient(Config{Address: address, Username: "operator", Password: ""}); err == nil {
		t.Fatal("newClient() error = nil, want validation error for missing password")
	}
}

func TestNewClientRejectsInvalidCAData(t *testing.T) {
	t.Parallel()

	_, err := newClient(Config{
		Address:  mustHTTPURL(t, "https://amt.test.walnuts.dev"),
		Username: "operator",
		Password: "secret",
		CAData:   []byte("not a certificate"),
	})
	if err == nil {
		t.Fatal("newClient() error = nil, want invalid PEM error")
	}
}

func TestNewClientAcceptsValidConfig(t *testing.T) {
	t.Parallel()

	address := mustHTTPURL(t, "http://amt.test.walnuts.dev:16992/wsman")
	if _, err := newClient(Config{Address: address, Username: "operator", Password: "secret"}); err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
}

// dmtfPowerOffSoftのregression test。go-wsman-messagesのRequestPowerStateChangeは受け取ったPowerStateを
// そのままfmt.Sprintf("%d", ...)でXMLへ埋め込むため、この値がDMTFのsoft power off(8)からずれると、
// 実機に対して意図しない電源操作(例えばPower Cycleに相当する値)を送ってしまう。
func TestDMTFPowerStateValues(t *testing.T) {
	t.Parallel()

	if dmtfPowerOn != 2 {
		t.Fatalf("dmtfPowerOn = %d, want 2", dmtfPowerOn)
	}
	if dmtfPowerOffSoft != 8 {
		t.Fatalf("dmtfPowerOffSoft = %d, want 8", dmtfPowerOffSoft)
	}
	if dmtfPowerMasterBusReset != 10 {
		t.Fatalf("dmtfPowerMasterBusReset = %d, want 10", dmtfPowerMasterBusReset)
	}

	if got := fmt.Sprintf("%d", wsmanpower.PowerState(dmtfPowerOffSoft)); got != "8" {
		t.Fatalf("wire representation of dmtfPowerOffSoft = %q, want %q", got, "8")
	}
}

func TestCheckReturnValue(t *testing.T) {
	t.Parallel()

	if err := checkReturnValue(0); err != nil {
		t.Fatalf("checkReturnValue(0) error = %v, want nil", err)
	}
	if err := checkReturnValue(1); !errors.Is(err, ErrProtocol) {
		t.Fatalf("checkReturnValue(1) error = %v, want ErrProtocol", err)
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "nil", err: nil, want: nil},
		{
			name: "SOAP Fault",
			err:  amterror.NewAMTError("b:DestinationUnreachable", "no route", ""),
			want: ErrProtocol,
		},
		{
			name: "接続エラー",
			err:  &url.Error{Op: "Post", URL: "https://amt.test.walnuts.dev/wsman", Err: errors.New("dial tcp: no such host")},
			want: ErrConnectionFailed,
		},
		{
			name: "HTTP 401",
			err:  fmt.Errorf("wsman.Client post received: %v\n%v", "401 Unauthorized", ""),
			want: ErrAuthenticationFailed,
		},
		{
			name: "HTTP 403",
			err:  fmt.Errorf("wsman.Client post received: %v\n%v", "403 Forbidden", ""),
			want: ErrAuthenticationFailed,
		},
		{
			name: "HTTP 500",
			err:  fmt.Errorf("wsman.Client post received: %v\n%v", "500 Internal Server Error", ""),
			want: ErrProtocol,
		},
		{
			name: "未知のエラーはfail-closedでProtocol",
			err:  errors.New("something unexpected"),
			want: ErrProtocol,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := classifyError(test.err)
			if test.want == nil {
				if got != nil {
					t.Fatalf("classifyError(%v) = %v, want nil", test.err, got)
				}
				return
			}
			if !errors.Is(got, test.want) {
				t.Fatalf("classifyError(%v) = %v, want wrapping %v", test.err, got, test.want)
			}
		})
	}
}

func TestGoWSManClientHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	// 実際のwsman.Messagesはzero-valueのまま保持する。ctx事前cancel時にネットワーク操作へ進めば
	// nil pointerでpanicするため、panicせずctx.Err()相当を返すことがネットワーク未発行の証拠になる。
	client := &goWSManClient{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := client.currentPowerState(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("currentPowerState() error = %v, want context.Canceled", err)
	}
	if err := client.requestPowerStateChange(ctx, dmtfPowerOn); !errors.Is(err, context.Canceled) {
		t.Fatalf("requestPowerStateChange() error = %v, want context.Canceled", err)
	}
}
