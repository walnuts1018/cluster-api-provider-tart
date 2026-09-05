package network

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

var ErrInvalidUDPAddress = errors.New("invalid UDP address")

// UDPAddressはIPアドレスとUDPポートを組み合わせた送信先を表す値オブジェクトである。
type UDPAddress string

// ParseUDPAddressはIPだけの入力にWake-on-LAN標準ポート9を補う。
func ParseUDPAddress(value string) (UDPAddress, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	parsed, err := netip.ParseAddrPort(value)
	if err != nil {
		address, addressErr := netip.ParseAddr(value)
		if addressErr != nil {
			return "", fmt.Errorf("%w: %q", ErrInvalidUDPAddress, value)
		}
		parsed = netip.AddrPortFrom(address, 9)
	}
	if parsed.Port() == 0 {
		return "", fmt.Errorf("%w: port must be greater than zero", ErrInvalidUDPAddress)
	}
	return UDPAddress(parsed.String()), nil
}

// IsZeroはUDP宛先が未設定かを返す。
func (address UDPAddress) IsZero() bool {
	return address == ""
}

// Stringはnetipが定義する正規化形式を返す。
func (address UDPAddress) String() string {
	return string(address)
}

func (address UDPAddress) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, address.String()), nil
}

func (address *UDPAddress) UnmarshalJSON(value []byte) error {
	return unmarshalTextJSON(value, address.UnmarshalText)
}

func (address UDPAddress) MarshalText() ([]byte, error) {
	return []byte(address.String()), nil
}

func (address *UDPAddress) UnmarshalText(value []byte) error {
	parsed, err := ParseUDPAddress(string(value))
	if err != nil {
		return err
	}
	*address = parsed
	return nil
}
