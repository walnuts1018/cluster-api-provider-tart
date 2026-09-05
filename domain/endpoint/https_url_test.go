package endpoint

import (
	"errors"
	"testing"
)

func TestParseHTTPSURL(t *testing.T) {
	t.Parallel()

	parsed, err := ParseHTTPSURL(" https://bmc.test.walnuts.dev/redfish/v1 ")
	if err != nil {
		t.Fatalf("ParseHTTPSURL() error = %v", err)
	}
	if parsed.String() != "https://bmc.test.walnuts.dev/redfish/v1" {
		t.Errorf("HTTPSURL.String() = %q", parsed)
	}
	if _, err := ParseHTTPSURL("http://bmc.test.walnuts.dev"); !errors.Is(err, ErrInvalidHTTPSURL) {
		t.Errorf("ParseHTTPSURL() error = %v, want ErrInvalidHTTPSURL", err)
	}
}
