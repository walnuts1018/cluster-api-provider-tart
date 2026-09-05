package network

import (
	"errors"
	"testing"
)

func TestParseUDPAddressNormalizesDefaultPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  UDPAddress
	}{
		{input: "255.255.255.255", want: "255.255.255.255:9"},
		{input: "192.0.2.1:7", want: "192.0.2.1:7"},
		{input: "[2001:db8::1]:9", want: "[2001:db8::1]:9"},
	}
	for _, tt := range tests {
		got, err := ParseUDPAddress(tt.input)
		if err != nil {
			t.Fatalf("ParseUDPAddress(%q) error = %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("ParseUDPAddress(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
	for _, input := range []string{"example.test:9", "192.0.2.1:0"} {
		if _, err := ParseUDPAddress(input); !errors.Is(err, ErrInvalidUDPAddress) {
			t.Errorf("ParseUDPAddress(%q) error = %v, want ErrInvalidUDPAddress", input, err)
		}
	}
}
