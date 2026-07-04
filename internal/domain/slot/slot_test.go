package slot

import (
	"errors"
	"testing"
)

func TestInactive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		active Slot
		want   Slot
	}{
		{name: "Aの反対はB", active: A, want: B},
		{name: "Bの反対はA", active: B, want: A},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.active.Inactive()
			if err != nil {
				t.Fatalf("Inactive() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Inactive() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlotRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	if _, err := Parse("C"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Parse() error = %v, want %v", err, ErrUnknown)
	}
	if _, err := Slot("C").Inactive(); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Inactive() error = %v, want %v", err, ErrUnknown)
	}
}
