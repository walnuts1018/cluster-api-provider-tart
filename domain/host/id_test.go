package host

import (
	"errors"
	"testing"
)

func TestParseHostIDAndProviderID(t *testing.T) {
	t.Parallel()

	hostID, err := ParseHostID("018F3C5E-5F8A-7C1B-9A2D-123456789ABC")
	if err != nil {
		t.Fatalf("ParseHostID() error = %v", err)
	}
	if got, want := hostID.String(), "018f3c5e-5f8a-7c1b-9a2d-123456789abc"; got != want {
		t.Errorf("HostID.String() = %q, want %q", got, want)
	}
	if _, err := ParseHostID("00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("ParseHostID() error = %v, want ErrInvalidID", err)
	}

	providerID, err := NewProviderID(hostID)
	if err != nil {
		t.Fatalf("NewProviderID() error = %v", err)
	}
	parsed, err := ParseProviderID(providerID.String())
	if err != nil || parsed != providerID {
		t.Errorf("ParseProviderID() = %q, %v, want %q", parsed, err, providerID)
	}
}
