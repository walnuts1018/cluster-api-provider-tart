package boot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

const (
	defaultWakeOnLANBroadcastAddress = "255.255.255.255:9"
	magicPacketHeaderSize            = 6
	magicPacketRepeatCount           = 16
)

// WakeOnLANはWake-on-LANマジックパケットを送信してHostの電源投入を要求する。電源投入や停止を確認できないため、停止確認はShutdown RPC受理後にauthenticated Talos APIが到達不能になることへ依存するが、物理的な電源断の証明にはならない。詳細は.agents/skills/host-lifecycle/SKILL.mdを参照する。
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

// PowerOnはマジックパケットを送信する。成功が確認する範囲はPowerOnインターフェースの説明に従う。
func (w WakeOnLAN) PowerOn(ctx context.Context) error {
	packet, err := magicPacket(w.macAddress)
	if err != nil {
		return err
	}

	dialer := &net.Dialer{Control: enableBroadcast}
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

func enableBroadcast(_, _ string, connection syscall.RawConn) error {
	var controlErr error
	if err := connection.Control(func(fileDescriptor uintptr) {
		controlErr = syscall.SetsockoptInt(int(fileDescriptor), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return controlErr
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
