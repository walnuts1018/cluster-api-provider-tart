// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
}

func NewHostTarget(bootMACAddress MACAddress) HostTarget {
	return HostTarget{bootMACAddress: bootMACAddress}
}

func (target HostTarget) BootMACAddress() MACAddress {
	return target.bootMACAddress
}
