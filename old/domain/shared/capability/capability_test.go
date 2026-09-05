package capability

import (
	"errors"
	"slices"
	"testing"
)

func TestCapabilityParse(t *testing.T) {
	t.Parallel()

	for _, capability := range All() {
		t.Run(string(capability), func(t *testing.T) {
			t.Parallel()

			got, err := Parse(string(capability))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got != capability {
				t.Fatalf("Parse() = %q, want %q", got, capability)
			}
		})
	}

	if _, err := Parse("NetworkBoot"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Parse() error = %v, want %v", err, ErrUnknown)
	}
}

func TestSet(t *testing.T) {
	t.Parallel()

	available, err := NewSet(PowerOn, SetNextBoot, PowerOn)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	required, err := NewSet(PowerOn, SetNextBoot)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	unsupported, err := NewSet(PowerOn, PowerOff)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	if !available.ContainsAll(required) {
		t.Fatal("ContainsAll() = false, want true")
	}
	if available.ContainsAll(unsupported) {
		t.Fatal("ContainsAll() = true, want false")
	}
	if got := available.Values(); !slices.Equal(got, []Capability{PowerOn, SetNextBoot}) {
		t.Fatalf("Values() = %v, want [PowerOn SetNextBoot]", got)
	}
}

func TestNewSetRejectsUnknownCapability(t *testing.T) {
	t.Parallel()

	if _, err := NewSet(Capability("unknown")); !errors.Is(err, ErrUnknown) {
		t.Fatalf("NewSet() error = %v, want %v", err, ErrUnknown)
	}
}
