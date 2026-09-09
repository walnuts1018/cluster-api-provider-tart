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
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
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
