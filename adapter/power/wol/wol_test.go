package wol

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestMagicPacket構造(t *testing.T) {
	macAddress, err := network.ParseMACAddress("00-00-5E-00-53-01")
	if err != nil {
		t.Fatalf("MACアドレスの解析に失敗: %v", err)
	}

	packet, err := magicPacket(macAddress)
	if err != nil {
		t.Fatalf("magicPacket() error = %v", err)
	}

	want := append(bytes.Repeat([]byte{0xff}, magicPacketHeaderSize), bytes.Repeat(macAddress.Bytes(), magicPacketRepeatCount)...)
	if !bytes.Equal(packet, want) {
		t.Fatalf("magicPacket() = %x, want %x", packet, want)
	}
	if got, want := len(packet), magicPacketHeaderSize+6*magicPacketRepeatCount; got != want {
		t.Fatalf("マジックパケットの長さ = %d, want %d", got, want)
	}
}

func TestMagicPacket不正MACアドレス(t *testing.T) {
	_, err := magicPacket(network.MACAddress{})
	if !errors.Is(err, network.ErrInvalidMACAddress) {
		t.Fatalf("magicPacket() error = %v, want ErrInvalidMACAddress", err)
	}
}

func TestNew不正MACアドレス(t *testing.T) {
	_, err := New(network.MACAddress{}, network.UDPAddress(""))
	if !errors.Is(err, network.ErrInvalidMACAddress) {
		t.Fatalf("New() error = %v, want ErrInvalidMACAddress", err)
	}
}

func TestPowerOnキャンセル済みContext(t *testing.T) {
	macAddress, err := network.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("MACアドレスの解析に失敗: %v", err)
	}
	broadcastAddress, err := network.ParseUDPAddress("127.0.0.1:9")
	if err != nil {
		t.Fatalf("UDPアドレスの解析に失敗: %v", err)
	}
	backend, err := New(macAddress, broadcastAddress)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := backend.PowerOn(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("PowerOn() error = %v, want context.Canceled", err)
	}
}
