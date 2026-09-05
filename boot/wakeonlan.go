package boot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
)

const (
	defaultWakeOnLANBroadcastAddress = "255.255.255.255:9"
	magicPacketHeaderSize            = 6
	magicPacketRepeatCount           = 16
)

// WakeOnLAN sends a Wake-on-LAN magic packet to power on a Host. It cannot confirm
// power-on or observe stop; stop confirmation for this backend relies on the
// authenticated Talos API becoming unreachable after a Shutdown RPC is accepted, which
// is not proof of physical power-off. See .agents/skills/host-lifecycle/SKILL.md.
type WakeOnLAN struct {
	macAddress       string
	broadcastAddress string
}

// NewWakeOnLAN returns a WakeOnLAN backend for the given MAC address. broadcastAddress
// defaults to the standard UDP discard port broadcast if empty.
func NewWakeOnLAN(macAddress, broadcastAddress string) WakeOnLAN {
	if broadcastAddress == "" {
		broadcastAddress = defaultWakeOnLANBroadcastAddress
	}
	return WakeOnLAN{macAddress: macAddress, broadcastAddress: broadcastAddress}
}

// PowerOn sends the magic packet. See PowerOn interface documentation for what success
// does and does not confirm.
func (w WakeOnLAN) PowerOn(ctx context.Context) error {
	packet, err := magicPacket(w.macAddress)
	if err != nil {
		return err
	}

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", w.broadcastAddress)
	if err != nil {
		return fmt.Errorf("dial wake-on-lan address: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("send wake-on-lan magic packet: %w", err),
				fmt.Errorf("close wake-on-lan connection: %w", closeErr),
			)
		}
		return fmt.Errorf("send wake-on-lan magic packet: %w", err)
	}
	return conn.Close()
}

func magicPacket(macAddress string) ([]byte, error) {
	hardwareAddress, err := net.ParseMAC(macAddress)
	if err != nil {
		return nil, fmt.Errorf("parse mac address: %w", err)
	}
	if len(hardwareAddress) != 6 {
		return nil, fmt.Errorf("mac address must be 6 bytes: %q", macAddress)
	}

	packet := make([]byte, 0, magicPacketHeaderSize+len(hardwareAddress)*magicPacketRepeatCount)
	packet = append(packet, bytes.Repeat([]byte{0xff}, magicPacketHeaderSize)...)
	packet = append(packet, bytes.Repeat(hardwareAddress, magicPacketRepeatCount)...)
	return packet, nil
}
