package network

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

var ErrInvalidMACAddress = errors.New("invalid MAC address")

// MACAddressは正規化済みのEUI-48を表す値オブジェクトである。
type MACAddress string

// ParseMACAddressは区切り文字の違いを許容してMACアドレスを正規化する。
func ParseMACAddress(value string) (MACAddress, error) {
	parsed, err := net.ParseMAC(strings.TrimSpace(value))
	if err != nil || len(parsed) != 6 {
		return "", fmt.Errorf("%w: %q", ErrInvalidMACAddress, value)
	}
	return MACAddress(strings.ToLower(parsed.String())), nil
}

// IsZeroはMACアドレスが未設定かを返す。
func (address MACAddress) IsZero() bool {
	return address == ""
}

// Stringは正規化済みの小文字コロン区切り形式を返す。
func (address MACAddress) String() string {
	return string(address)
}

// BytesはMACアドレスをパケットへコピーするためのバイト列に変換する。
func (address MACAddress) Bytes() []byte {
	if address.IsZero() {
		return nil
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(address.String(), ":", ""))
	if err != nil {
		return nil
	}
	return decoded
}

func (address MACAddress) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, address.String()), nil
}

func (address *MACAddress) UnmarshalJSON(value []byte) error {
	return unmarshalTextJSON(value, address.UnmarshalText)
}

func (address MACAddress) MarshalText() ([]byte, error) {
	return []byte(address.String()), nil
}

func (address *MACAddress) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		*address = ""
		return nil
	}
	parsed, err := ParseMACAddress(string(value))
	if err != nil {
		return err
	}
	*address = parsed
	return nil
}
