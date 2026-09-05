package driver

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
)

var ErrInvalidMACAddress = errors.New("invalid MAC address")

type MACAddress struct {
	octets [6]byte
}

func ParseMACAddress(value string) (MACAddress, error) {
	parsed, err := net.ParseMAC(value)
	if err != nil || len(parsed) != 6 {
		return MACAddress{}, fmt.Errorf("%w: %q", ErrInvalidMACAddress, value)
	}

	var octets [6]byte
	copy(octets[:], parsed)
	return MACAddress{octets: octets}, nil
}

func (address MACAddress) String() string {
	encoded := make([]byte, hex.EncodedLen(len(address.octets)))
	hex.Encode(encoded, address.octets[:])
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		encoded[0:2],
		encoded[2:4],
		encoded[4:6],
		encoded[6:8],
		encoded[8:10],
		encoded[10:12],
	)
}

type HostTarget struct {
	bootMACAddress MACAddress
	redfishAccess  *RedfishAccess
}

func NewHostTarget(bootMACAddress MACAddress) HostTarget {
	return HostTarget{bootMACAddress: bootMACAddress}
}

func (target HostTarget) BootMACAddress() MACAddress {
	return target.bootMACAddress
}

func (target HostTarget) WithRedfishAccess(access RedfishAccess) HostTarget {
	target.redfishAccess = &access
	return target
}

func (target HostTarget) RedfishAccess() (RedfishAccess, bool) {
	if target.redfishAccess == nil {
		return RedfishAccess{}, false
	}
	return *target.redfishAccess, true
}
