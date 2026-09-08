package endpoint

import (
	"errors"
	"testing"
)

func TestParseHTTPURL(t *testing.T) {
	t.Parallel()

	parsed, err := ParseHTTPURL(" http://amt.test.walnuts.dev:16992/wsman ")
	if err != nil {
		t.Fatalf("ParseHTTPURL() error = %v", err)
	}
	if parsed.String() != "http://amt.test.walnuts.dev:16992/wsman" {
		t.Errorf("HTTPURL.String() = %q", parsed)
	}

	if _, err := ParseHTTPURL("https://amt.test.walnuts.dev:16993/wsman"); err != nil {
		t.Errorf("ParseHTTPURL() error = %v, want nil for https scheme", err)
	}

	if _, err := ParseHTTPURL("ftp://amt.test.walnuts.dev"); !errors.Is(err, ErrInvalidHTTPURL) {
		t.Errorf("ParseHTTPURL() error = %v, want ErrInvalidHTTPURL", err)
	}
	if _, err := ParseHTTPURL("http://user:pass@amt.test.walnuts.dev"); !errors.Is(err, ErrInvalidHTTPURL) {
		t.Errorf("ParseHTTPURL() error = %v, want ErrInvalidHTTPURL for userinfo", err)
	}
	if _, err := ParseHTTPURL("http://amt.test.walnuts.dev?query=1"); !errors.Is(err, ErrInvalidHTTPURL) {
		t.Errorf("ParseHTTPURL() error = %v, want ErrInvalidHTTPURL for query", err)
	}
}
