package host

import (
	"errors"
	"testing"
)

func TestNewProviderID(t *testing.T) {
	t.Parallel()

	id, err := ParseHostID("018f3c5e-5f8a-7c1b-9a2d-123456789abc")
	if err != nil {
		t.Fatalf("ParseHostID() error = %v", err)
	}

	got, err := NewProviderID(id)
	if err != nil {
		t.Fatalf("NewProviderID() error = %v", err)
	}
	want := ProviderID("tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc")
	if got != want {
		t.Errorf("NewProviderID() = %q, want %q", got, want)
	}

	if _, err := NewProviderID(HostID{}); !errors.Is(err, ErrInvalidProviderID) {
		t.Errorf("NewProviderID(zero) error = %v, want ErrInvalidProviderID", err)
	}
}

func TestParseProviderID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{
			name:  "valid provider ID round-trips",
			value: "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc",
			want:  "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc",
		},
		{
			name:  "surrounding whitespace is trimmed",
			value: "  tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc  ",
			want:  "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc",
		},
		{
			name:    "missing prefix is rejected",
			value:   "018f3c5e-5f8a-7c1b-9a2d-123456789abc",
			wantErr: true,
		},
		{
			name:    "wrong scheme is rejected",
			value:   "aws:///us-east-1a/i-1234",
			wantErr: true,
		},
		{
			name:    "invalid host ID suffix is rejected",
			value:   "tart://host/not-a-uuid",
			wantErr: true,
		},
		{
			name:    "empty value is rejected",
			value:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseProviderID(tt.value)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidProviderID) {
					t.Errorf("ParseProviderID(%q) error = %v, want ErrInvalidProviderID", tt.value, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseProviderID(%q) error = %v", tt.value, err)
			}
			if got.String() != tt.want {
				t.Errorf("ParseProviderID(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestProviderIDJSONRoundTrip(t *testing.T) {
	t.Parallel()

	id, err := ParseProviderID("tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc")
	if err != nil {
		t.Fatalf("ParseProviderID() error = %v", err)
	}

	data, err := id.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	var got ProviderID
	if err := got.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if got != id {
		t.Errorf("UnmarshalJSON() = %q, want %q", got, id)
	}
}

func TestProviderIDUnmarshalTextEmpty(t *testing.T) {
	t.Parallel()

	var id ProviderID = "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc"
	if err := id.UnmarshalText(nil); err != nil {
		t.Fatalf("UnmarshalText(nil) error = %v", err)
	}
	if !id.IsZero() {
		t.Errorf("UnmarshalText(nil) left id = %q, want zero value", id)
	}
}

func TestProviderIDUnmarshalTextInvalid(t *testing.T) {
	t.Parallel()

	var id ProviderID
	if err := id.UnmarshalText([]byte("not-a-provider-id")); !errors.Is(err, ErrInvalidProviderID) {
		t.Errorf("UnmarshalText(invalid) error = %v, want ErrInvalidProviderID", err)
	}
}
