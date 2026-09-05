package network

import (
	"errors"
	"testing"
)

func TestParseMACAddressNormalizesAndRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    MACAddress
		wantErr bool
	}{
		{name: "colon separated", input: "00:00:5E:00:53:02", want: "00:00:5e:00:53:02"},
		{name: "hyphen separated", input: "00-00-5e-00-53-02", want: "00:00:5e:00:53:02"},
		{name: "EUI-64 is rejected", input: "00:00:5e:ff:fe:00:53:02", wantErr: true},
		{name: "invalid value is rejected", input: "not-a-mac", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMACAddress(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseMACAddress() error = %v, wantErr %t", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseMACAddress() = %q, want %q", got, tt.want)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidMACAddress) {
				t.Errorf("ParseMACAddress() error = %v, want ErrInvalidMACAddress", err)
			}
		})
	}
}
