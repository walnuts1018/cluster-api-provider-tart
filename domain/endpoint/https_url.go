package endpoint

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var ErrInvalidHTTPSURL = errors.New("invalid HTTPS URL")

// HTTPSURLはHTTPSスキームとホストを持つURLを表す値オブジェクトである。
type HTTPSURL string

// ParseHTTPSURLはRedfishエンドポイントなどのHTTPS URLを検証する。
func ParseHTTPSURL(value string) (HTTPSURL, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidHTTPSURL, value)
	}
	return HTTPSURL(parsed.String()), nil
}

func (endpoint HTTPSURL) String() string {
	return string(endpoint)
}

func (endpoint HTTPSURL) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, endpoint.String()), nil
}

func (endpoint *HTTPSURL) UnmarshalJSON(value []byte) error {
	return unmarshalTextJSON(value, endpoint.UnmarshalText)
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

func unmarshalTextJSON(value []byte, unmarshal func([]byte) error) error {
	text, err := strconv.Unquote(string(value))
	if err != nil {
		return err
	}
	return unmarshal([]byte(text))
}
