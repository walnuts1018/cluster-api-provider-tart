// Package wolはWake-on-LAN電源backendの実装を提供する。
package wol

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

// BackendはWake-on-LANマジックパケットを送信してHostの電源投入を要求する。電源投入や停止を確認できないため、停止確認はShutdown RPC受理後にauthenticated Talos APIが到達不能になることへ依存するが、物理的な電源断の証明にはならない。詳細は.agents/skills/host-lifecycle/SKILL.mdを参照する。
type Backend struct {
	macAddress       network.MACAddress
	broadcastAddress network.UDPAddress
}

// Newは検証済みのMACアドレスとUDP送信先からWake-on-LAN backendを構築する。
func New(macAddress network.MACAddress, broadcastAddress network.UDPAddress) (Backend, error) {
	if macAddress.IsZero() {
		return Backend{}, fmt.Errorf("validate wake-on-LAN MAC address: %w", network.ErrInvalidMACAddress)
	}
	var err error
	if broadcastAddress.IsZero() {
		broadcastAddress, err = network.ParseUDPAddress(defaultWakeOnLANBroadcastAddress)
		if err != nil {
			return Backend{}, fmt.Errorf("parse default wake-on-LAN address: %w", err)
		}
	} else {
		parsedBroadcast, parseErr := network.ParseUDPAddress(broadcastAddress.String())
		if parseErr != nil {
			return Backend{}, fmt.Errorf("validate wake-on-LAN broadcast address: %w", parseErr)
		}
		broadcastAddress = parsedBroadcast
	}
	return Backend{macAddress: macAddress, broadcastAddress: broadcastAddress}, nil
}

// PowerOnはマジックパケットを送信する。成功が確認する範囲はPowerOnインターフェースの説明に従う。
func (w Backend) PowerOn(ctx context.Context) error {
	packet, err := magicPacket(w.macAddress)
	if err != nil {
		return err
	}

	dialer := &net.Dialer{Control: enableBroadcast}
	conn, err := dialer.DialContext(ctx, "udp", w.broadcastAddress.String())
	if err != nil {
		return fmt.Errorf("dial wake-on-LAN address: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("send wake-on-LAN magic packet: %w", err),
				fmt.Errorf("close wake-on-LAN connection: %w", closeErr),
			)
		}
		return fmt.Errorf("send wake-on-LAN magic packet: %w", err)
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
	if macAddress.IsZero() {
		return nil, fmt.Errorf("validate MAC address for magic packet: %w", network.ErrInvalidMACAddress)
	}

	hardwareAddress := macAddress.Bytes()
	packet := make([]byte, 0, magicPacketHeaderSize+len(hardwareAddress)*magicPacketRepeatCount)
	packet = append(packet, bytes.Repeat([]byte{0xff}, magicPacketHeaderSize)...)
	packet = append(packet, bytes.Repeat(hardwareAddress, magicPacketRepeatCount)...)
	return packet, nil
}
