package wol

import (
	"bytes"
	"errors"
	"fmt"
	"net"
)

const (
	defaultBroadcastAddress = "255.255.255.255:9"
	magicPacketHeaderSize   = 6
	magicPacketRepeatCount  = 16
)

// MagicPacket は Wake-on-LAN で使う Magic Packet を生成します。
func MagicPacket(macAddress string) ([]byte, error) {
	hardwareAddress, err := net.ParseMAC(macAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mac address: %w", err)
	}
	if len(hardwareAddress) != 6 {
		return nil, fmt.Errorf("mac address must be 6 bytes: %q", macAddress)
	}

	packet := make([]byte, 0, magicPacketHeaderSize+len(hardwareAddress)*magicPacketRepeatCount)
	packet = append(packet, bytes.Repeat([]byte{0xff}, magicPacketHeaderSize)...)
	packet = append(packet, bytes.Repeat(hardwareAddress, magicPacketRepeatCount)...)
	return packet, nil
}

// Sender は Wake-on-LAN Magic Packet を送信します。
type Sender struct {
	address string
}

// DefaultSender は標準の UDP discard port へ broadcast する Sender を返します。
func DefaultSender() Sender {
	return Sender{address: defaultBroadcastAddress}
}

// NewSender は任意の送信先へ Magic Packet を送信する Sender を返します。
func NewSender(address string) Sender {
	return Sender{address: address}
}

// Send は指定した MAC アドレスへ Magic Packet を送信します。
func (s Sender) Send(macAddress string) error {
	packet, err := MagicPacket(macAddress)
	if err != nil {
		return err
	}

	conn, err := net.Dial("udp", s.address)
	if err != nil {
		return fmt.Errorf("failed to dial wol address: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("failed to send wol magic packet: %w", err),
				fmt.Errorf("failed to close wol connection: %w", closeErr),
			)
		}
		return fmt.Errorf("failed to send wol magic packet: %w", err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("failed to close wol connection: %w", err)
	}
	return nil
}
