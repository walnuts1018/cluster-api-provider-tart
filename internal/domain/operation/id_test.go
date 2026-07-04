package operation

import (
	"errors"
	"testing"
)

func TestParseID(t *testing.T) {
	t.Parallel()

	const value = "0197d640-8d00-7a65-b67f-3f7c42a6935f"
	id, err := ParseID(value)
	if err != nil {
		t.Fatalf("ParseID() error = %v", err)
	}
	if !id.Valid() {
		t.Fatal("ID.Valid() = false, want true")
	}
	if id.String() != value {
		t.Fatalf("ID.String() = %q, want %q", id.String(), value)
	}
}

func TestParseIDRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseID(value); !errors.Is(err, ErrInvalidID) {
				t.Fatalf("ParseID() error = %v, want %v", err, ErrInvalidID)
			}
		})
	}
}

func TestDeterministicIDIsStableForTheSameKey(t *testing.T) {
	t.Parallel()

	first, err := DeterministicID("host-uid/machine-uid")
	if err != nil {
		t.Fatalf("first DeterministicID() error = %v", err)
	}
	second, err := DeterministicID("host-uid/machine-uid")
	if err != nil {
		t.Fatalf("second DeterministicID() error = %v", err)
	}
	if first != second {
		t.Fatalf("DeterministicID() returned different IDs: %s != %s", first.String(), second.String())
	}
}

func TestDeterministicIDRejectsEmptyKey(t *testing.T) {
	t.Parallel()

	if _, err := DeterministicID(""); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("DeterministicID() error = %v, want ErrInvalidID", err)
	}
}
