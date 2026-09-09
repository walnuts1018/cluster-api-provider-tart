package endpoint

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/textmarshal"
)

var ErrInvalidHTTPSURL = errors.New("invalid HTTPS URL")

// HTTPSURLはHTTPSスキームとホストを持つURLを表す値オブジェクトである。
type HTTPSURL string

// ParseHTTPSURLはRedfishエンドポイントなどのHTTPS URLを検証する。
func ParseHTTPSURL(value string) (HTTPSURL, error) {
	value = strings.TrimSpace(value)
	// url.ParseRequestURIはHTTP request-URI(fragmentを含まない)を前提とするため、"#"以降を
	// パス等の一部として取り込んでしまいFragmentを検出できない。外部から渡された完全なURLを
	// 検証する用途にはRFC 3986準拠のurl.Parseを使う。
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidHTTPSURL, value)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidHTTPSURL, value)
	}
	return HTTPSURL(parsed.String()), nil
}

func (endpoint HTTPSURL) String() string {
	return string(endpoint)
}

// URLは検証済みのURLをnet/url.URLとして返す。ParseHTTPSURLを通過した値のみがHTTPSURLとして
// 存在するため、呼び出し側で検証を繰り返す必要はない。
func (endpoint HTTPSURL) URL() (*url.URL, error) {
	return url.Parse(endpoint.String())
}

func (endpoint HTTPSURL) MarshalJSON() ([]byte, error) {
	return textmarshal.JSON(endpoint.String())
}

func (endpoint *HTTPSURL) UnmarshalJSON(value []byte) error {
	return textmarshal.UnmarshalJSON(value, endpoint.UnmarshalText)
}

func (endpoint HTTPSURL) MarshalText() ([]byte, error) {
	return []byte(endpoint.String()), nil
}

func (endpoint *HTTPSURL) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		*endpoint = ""
		return nil
	}
	parsed, err := ParseHTTPSURL(string(value))
	if err != nil {
		return err
	}
	*endpoint = parsed
	return nil
}
