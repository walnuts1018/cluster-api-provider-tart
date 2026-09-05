package network

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

var ErrInvalidEndpoint = errors.New("invalid network endpoint")

// EndpointはTalos APIなどへ接続するホストまたはホストとポートの組み合わせを表す値オブジェクトである。
type Endpoint string

// ParseEndpointはスキームやパスを含まないホストエンドポイントを正規化する。
func ParseEndpoint(value string) (Endpoint, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n/?#") || strings.Contains(value, "://") {
		return "", fmt.Errorf("%w: %q", ErrInvalidEndpoint, value)
	}
	if address, err := netip.ParseAddrPort(value); err == nil {
		if address.Port() == 0 {
			return "", fmt.Errorf("%w: port must be greater than zero", ErrInvalidEndpoint)
		}
		return Endpoint(address.String()), nil
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return Endpoint(address.String()), nil
	}
	if host, portText, err := net.SplitHostPort(value); err == nil {
		port, parseErr := strconv.ParseUint(portText, 10, 16)
		if parseErr != nil || port == 0 || !validHostname(host) {
			return "", fmt.Errorf("%w: %q", ErrInvalidEndpoint, value)
		}
		return Endpoint(net.JoinHostPort(strings.ToLower(host), strconv.FormatUint(port, 10))), nil
	}
	if validHostname(value) {
		return Endpoint(strings.ToLower(value)), nil
	}
	return "", fmt.Errorf("%w: %q", ErrInvalidEndpoint, value)
}

func (endpoint Endpoint) IsZero() bool {
	return endpoint == ""
}

func (endpoint Endpoint) String() string {
	return string(endpoint)
}

func (endpoint Endpoint) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, endpoint.String()), nil
}

func (endpoint *Endpoint) UnmarshalJSON(value []byte) error {
	return unmarshalTextJSON(value, endpoint.UnmarshalText)
}

func (endpoint Endpoint) MarshalText() ([]byte, error) {
	return []byte(endpoint.String()), nil
}

func (endpoint *Endpoint) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		*endpoint = ""
		return nil
	}
	parsed, err := ParseEndpoint(string(value))
	if err != nil {
		return err
	}
	*endpoint = parsed
	return nil
}

func validHostname(value string) bool {
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	for label := range strings.SplitSeq(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}
