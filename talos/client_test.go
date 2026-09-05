package talos

import (
	"errors"
	"testing"
)

func TestClientVersionRejectsUnavailableClient(t *testing.T) {
	t.Parallel()

	var client *Client
	_, err := client.Version(t.Context())
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("Version() error = %v, want ErrClientUnavailable", err)
	}
}

func TestClientShutdownRejectsUnavailableClient(t *testing.T) {
	t.Parallel()

	var client *Client
	if err := client.Shutdown(t.Context()); !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("Shutdown() error = %v, want ErrClientUnavailable", err)
	}
}

func TestDialRejectsEmptyEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := DialMaintenance(t.Context(), " \t"); !errors.Is(err, ErrEndpointEmpty) {
		t.Fatalf("DialMaintenance() error = %v, want ErrEndpointEmpty", err)
	}
	if _, err := DialAuthenticated(t.Context(), "", nil, nil, nil); !errors.Is(err, ErrEndpointEmpty) {
		t.Fatalf("DialAuthenticated() error = %v, want ErrEndpointEmpty", err)
	}
}
