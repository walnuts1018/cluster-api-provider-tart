package driver

import (
	"errors"
	"testing"
)

func TestParseMACAddress(t *testing.T) {
	t.Parallel()

	address, err := ParseMACAddress("00-00-5E-00-53-02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	if got := address.String(); got != "00:00:5e:00:53:02" {
		t.Fatalf("MACAddress.String() = %q, want %q", got, "00:00:5e:00:53:02")
	}
}

func TestParseMACAddressRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseMACAddress("invalid"); !errors.Is(err, ErrInvalidMACAddress) {
		t.Fatalf("ParseMACAddress() error = %v, want ErrInvalidMACAddress", err)
	}
}
