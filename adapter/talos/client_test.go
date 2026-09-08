package talos

import (
	"context"
	"errors"
	"testing"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	talosmachine "github.com/siderolabs/talos/pkg/machinery/config/machine"
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

func TestDialAuthenticatedFromWorkerConfigurationDoesNotPanic(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	input, err := generate.NewInput(
		"cluster-a",
		"https://192.0.2.10:6443",
		"1.34.0",
		generate.WithSecretsBundle(bundle),
	)
	if err != nil {
		t.Fatalf("generate.NewInput() error = %v", err)
	}
	provider, err := input.Config(talosmachine.TypeWorker)
	if err != nil {
		t.Fatalf("Config(worker) error = %v", err)
	}
	configuration, err := provider.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		t.Fatalf("EncodeBytes() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("DialAuthenticatedFromConfiguration() panicked for worker configuration: %v", recovered)
		}
	}()
	client, err := DialAuthenticatedFromConfiguration(ctx, "127.0.0.1:50000", configuration)
	if client != nil {
		if closeErr := client.Close(); closeErr != nil {
			t.Fatalf("Client.Close() error = %v", closeErr)
		}
	}
	if err == nil && client == nil {
		t.Fatal("DialAuthenticatedFromConfiguration() returned neither a client nor an error")
	}
}

func TestValidateUpgradeUsesTalosCompatibilityRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		desired string
		valid   bool
	}{
		{name: "supported patch upgrade", current: "v1.14.0", desired: "v1.14.1", valid: true},
		{name: "modern configuration unavailable", current: "v1.13.0", desired: "v1.14.0"},
		{name: "lifecycle API unavailable", current: "v1.12.0", desired: "v1.13.0"},
		{name: "host too old", current: "v1.11.0", desired: "v1.14.0"},
		{name: "downgrade", current: "v1.14.0", desired: "v1.13.0"},
		{name: "invalid desired version", current: "v1.13.0", desired: "not-a-version"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateUpgrade(test.current, test.desired)
			if (err == nil) != test.valid {
				t.Fatalf("ValidateUpgrade(%q, %q) error = %v, valid = %t", test.current, test.desired, err, test.valid)
			}
		})
	}
}

func TestInstallerImageRejectsUnversionedTalosTag(t *testing.T) {
	t.Parallel()

	if _, err := InstallerImage("1.14.0", "schematic"); err == nil {
		t.Fatal("InstallerImage() error = nil, want version prefix validation")
	}
}

func TestInstallerImageRejectsMalformedSchematicID(t *testing.T) {
	t.Parallel()

	for _, schematicID := range []string{"factory/path", "schematic:tag", "schematic@digest", "schematic id"} {
		if _, err := InstallerImage("v1.14.0", schematicID); err == nil {
			t.Fatalf("InstallerImage(%q) error = nil, want schematic ID validation", schematicID)
		}
	}
}
