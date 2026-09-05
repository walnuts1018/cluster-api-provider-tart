package network

import (
	"errors"
	"testing"
)

func TestParseEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Endpoint
	}{
		{input: "192.0.2.1:50000", want: "192.0.2.1:50000"},
		{input: "[2001:db8::1]:50000", want: "[2001:db8::1]:50000"},
		{input: "Talos-01.test.walnuts.dev", want: "talos-01.test.walnuts.dev"},
	}
	for _, tt := range tests {
		got, err := ParseEndpoint(tt.input)
		if err != nil {
			t.Fatalf("ParseEndpoint(%q) error = %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("ParseEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
	for _, input := range []string{"", "https://talos.test.walnuts.dev", "talos.test.walnuts.dev/path", "talos.test.walnuts.dev:0"} {
		if _, err := ParseEndpoint(input); !errors.Is(err, ErrInvalidEndpoint) {
			t.Errorf("ParseEndpoint(%q) error = %v, want ErrInvalidEndpoint", input, err)
		}
	}
}
