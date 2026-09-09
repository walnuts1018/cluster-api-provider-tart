package endpoint

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/textmarshal"
)

var ErrInvalidHTTPURL = errors.New("invalid HTTP URL")

// HTTPURLはhttpまたはhttpsスキームとホストを持つURLを表す値オブジェクトである。
// Intel Manageabilityのようにhttp/https双方のendpointを許容する必要があるbackend向けに、https限定のHTTPSURLとは別型として用意する。
type HTTPURL string

// ParseHTTPURLはIntel Manageability endpointなどのhttp/https URLを検証する。
func ParseHTTPURL(value string) (HTTPURL, error) {
	value = strings.TrimSpace(value)
	// url.ParseRequestURIはHTTP request-URI(fragmentを含まない)を前提とするため、"#"以降を
	// パス等の一部として取り込んでしまいFragmentを検出できない。外部から渡された完全なURLを
	// 検証する用途にはRFC 3986準拠のurl.Parseを使う。
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidHTTPURL, value)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidHTTPURL, value)
	}
	return HTTPURL(parsed.String()), nil
}

func (endpoint HTTPURL) String() string {
	return string(endpoint)
}

// URLは検証済みのURLをnet/url.URLとして返す。ParseHTTPURLを通過した値のみがHTTPURLとして
// 存在するため、呼び出し側で検証を繰り返す必要はない。
func (endpoint HTTPURL) URL() (*url.URL, error) {
	return url.Parse(endpoint.String())
}

func (endpoint HTTPURL) MarshalJSON() ([]byte, error) {
	return textmarshal.JSON(endpoint.String())
}

func (endpoint *HTTPURL) UnmarshalJSON(value []byte) error {
	return textmarshal.UnmarshalJSON(value, endpoint.UnmarshalText)
}

func (endpoint HTTPURL) MarshalText() ([]byte, error) {
	return []byte(endpoint.String()), nil
}

func (endpoint *HTTPURL) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		*endpoint = ""
		return nil
	}
	parsed, err := ParseHTTPURL(string(value))
	if err != nil {
		return err
	}
	*endpoint = parsed
	return nil
}
