package boot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
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
	macAddress       network.MACAddress
	broadcastAddress network.UDPAddress
}

// NewWakeOnLANは検証済みのMACアドレスとUDP送信先からWake-on-LAN backendを構築する。
func NewWakeOnLAN(macAddress network.MACAddress, broadcastAddress network.UDPAddress) (WakeOnLAN, error) {
	parsedMAC, err := network.ParseMACAddress(macAddress.String())
	if err != nil {
		return WakeOnLAN{}, fmt.Errorf("validate wake-on-lan MAC address: %w", err)
	}
	macAddress = parsedMAC
	if broadcastAddress.IsZero() {
		broadcastAddress, err = network.ParseUDPAddress(defaultWakeOnLANBroadcastAddress)
		if err != nil {
			return WakeOnLAN{}, fmt.Errorf("parse default wake-on-lan address: %w", err)
		}
	} else {
		parsedBroadcast, parseErr := network.ParseUDPAddress(broadcastAddress.String())
		if parseErr != nil {
			return WakeOnLAN{}, fmt.Errorf("validate wake-on-lan broadcast address: %w", parseErr)
		}
		broadcastAddress = parsedBroadcast
	}
	return WakeOnLAN{macAddress: macAddress, broadcastAddress: broadcastAddress}, nil
}

// PowerOn sends the magic packet. See PowerOn interface documentation for what success
// does and does not confirm.
func (w WakeOnLAN) PowerOn(ctx context.Context) error {
	packet, err := magicPacket(w.macAddress)
	if err != nil {
		return err
	}

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", w.broadcastAddress.String())
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

func magicPacket(macAddress network.MACAddress) ([]byte, error) {
	parsedMAC, err := network.ParseMACAddress(macAddress.String())
	if err != nil {
		return nil, fmt.Errorf("validate MAC address for magic packet: %w", err)
	}

	hardwareAddress := parsedMAC.Bytes()
	packet := make([]byte, 0, magicPacketHeaderSize+len(hardwareAddress)*magicPacketRepeatCount)
	packet = append(packet, bytes.Repeat([]byte{0xff}, magicPacketHeaderSize)...)
	packet = append(packet, bytes.Repeat(hardwareAddress, magicPacketRepeatCount)...)
	return packet, nil
}
