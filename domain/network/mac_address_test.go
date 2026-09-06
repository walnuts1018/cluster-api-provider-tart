package network

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseMACAddressNormalizesAndRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "colon separated", input: "00:00:5E:00:53:02", want: "00:00:5e:00:53:02"},
		{name: "hyphen separated", input: "00-00-5e-00-53-02", want: "00:00:5e:00:53:02"},
		{name: "EUI-64 is rejected", input: "00:00:5e:ff:fe:00:53:02", wantErr: true},
		{name: "invalid value is rejected", input: "not-a-mac", wantErr: true},
		{name: "whitespace is rejected", input: " 00:00:5e:00:53:02 ", wantErr: true},
		{name: "zero MAC is valid", input: "00:00:00:00:00:00", want: "00:00:00:00:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMACAddress(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseMACAddress() error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidMACAddress) {
					t.Errorf("ParseMACAddress() error = %v, want ErrInvalidMACAddress", err)
				}
				return
			}
			if got.String() != tt.want {
				t.Errorf("ParseMACAddress() = %q, want %q", got.String(), tt.want)
			}
			if got.IsZero() {
				t.Errorf("ParseMACAddress() IsZero = true, want false")
			}
			wantParsed, _ := ParseMACAddress(tt.want)
			if got != wantParsed {
				t.Errorf("ParseMACAddress() comparable check failed: %v != %v", got, wantParsed)
			}
		})
	}
}

func TestMACAddressZeroDistinguishesFromZeroMAC(t *testing.T) {
	t.Parallel()

	var zero MACAddress
	if !zero.IsZero() {
		t.Fatalf("zero MACAddress IsZero = false, want true")
	}
	if zero.String() != "" {
		t.Errorf("zero MACAddress String = %q, want empty", zero.String())
	}
	if zero.Bytes() != nil {
		t.Errorf("zero MACAddress Bytes = %v, want nil", zero.Bytes())
	}
	if _, ok := zero.Array(); ok {
		t.Errorf("zero MACAddress Array ok = true, want false")
	}

	zeroMAC, err := ParseMACAddress("00:00:00:00:00:00")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	if zeroMAC.IsZero() {
		t.Fatalf("00:00:00:00:00:00 IsZero = true, want false")
	}
	if zeroMAC == zero {
		t.Fatalf("zero MACAddress == 00:00:00:00:00:00, want distinct")
	}
	if zeroMAC.String() != "00:00:00:00:00:00" {
		t.Errorf("zeroMAC String = %q, want %q", zeroMAC.String(), "00:00:00:00:00:00")
	}
	if got := zeroMAC.Bytes(); len(got) != 6 {
		t.Errorf("zeroMAC Bytes length = %d, want 6", len(got))
	}
}

func TestMACAddressJSONRoundTrip(t *testing.T) {
	t.Parallel()

	mac, err := ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}

	data, err := json.Marshal(mac)
	if err != nil {
		t.Fatalf("MarshalJSON error = %v", err)
	}
	if string(data) != `"00:00:5e:00:53:02"` {
		t.Errorf("MarshalJSON = %s, want %q", string(data), "00:00:5e:00:53:02")
	}

	var decoded MACAddress
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("UnmarshalJSON error = %v", err)
	}
	if decoded != mac {
		t.Errorf("UnmarshalJSON = %v, want %v", decoded, mac)
	}

	var zero MACAddress
	nullData := []byte("null")
	if err := json.Unmarshal(nullData, &zero); err != nil {
		t.Fatalf("UnmarshalJSON null error = %v", err)
	}
	if !zero.IsZero() {
		t.Errorf("UnmarshalJSON null IsZero = false, want true")
	}

	emptyData := []byte(`""`)
	var empty MACAddress
	if err := json.Unmarshal(emptyData, &empty); err != nil {
		t.Fatalf("UnmarshalJSON empty error = %v", err)
	}
	if !empty.IsZero() {
		t.Errorf("UnmarshalJSON empty IsZero = false, want true")
	}

	var zeroForMarshal MACAddress
	zeroData, err := json.Marshal(zeroForMarshal)
	if err != nil {
		t.Fatalf("MarshalJSON zero error = %v", err)
	}
	if string(zeroData) != `""` {
		t.Errorf("MarshalJSON zero = %s, want %q", string(zeroData), "")
	}
}

func TestMACAddressTextRoundTrip(t *testing.T) {
	t.Parallel()

	mac, _ := ParseMACAddress("00:00:5e:00:53:02")
	text, err := mac.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error = %v", err)
	}
	if string(text) != "00:00:5e:00:53:02" {
		t.Errorf("MarshalText = %q, want %q", string(text), "00:00:5e:00:53:02")
	}

	var decoded MACAddress
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText error = %v", err)
	}
	if decoded != mac {
		t.Errorf("UnmarshalText = %v, want %v", decoded, mac)
	}

	var zero MACAddress
	if err := zero.UnmarshalText([]byte("")); err != nil {
		t.Fatalf("UnmarshalText empty error = %v", err)
	}
	if !zero.IsZero() {
		t.Errorf("UnmarshalText empty IsZero = false, want true")
	}
}
