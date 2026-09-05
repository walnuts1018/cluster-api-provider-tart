package cluster

import (
	"errors"
	"testing"
)

func TestParseClusterID(t *testing.T) {
	t.Parallel()

	id, err := ParseClusterID("018F3C5E-5F8A-7C1B-9A2D-123456789ABC")
	if err != nil {
		t.Fatalf("ParseClusterID() error = %v", err)
	}
	if got, want := id.String(), "018f3c5e-5f8a-7c1b-9a2d-123456789abc"; got != want {
		t.Errorf("ClusterID.String() = %q, want %q", got, want)
	}
	if _, err := ParseClusterID("00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("ParseClusterID() error = %v, want ErrInvalidID", err)
	}
}
