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

func TestValidateUpgradeUsesTalosCompatibilityRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		desired string
		valid   bool
	}{
		{name: "supported minor upgrade", current: "v1.13.0", desired: "v1.14.0", valid: true},
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
