package wol_test

import (
	"bytes"
	"net"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/wol"
)

func TestMagicPacket(t *testing.T) {
	t.Parallel()

	packet, err := wol.MagicPacket("00:11:22:33:44:55")
	if err != nil {
		t.Fatalf("MagicPacket() error = %v", err)
	}

	want := append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat([]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}, 16)...)
	if !bytes.Equal(packet, want) {
		t.Fatalf("MagicPacket() = %x, want %x", packet, want)
	}
}

func TestSend(t *testing.T) {
	t.Parallel()

	// ローカル UDP ポートを開いて Magic Packet を受信する
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket() error = %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("failed to close listener: %v", err)
		}
	}()

	addr := conn.LocalAddr().String()
	sender := wol.NewSender(addr)

	const mac = "00:11:22:33:44:55"
	if err := sender.Send(mac); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// 受信した内容が正しい Magic Packet であることを確認する
	buf := make([]byte, 102)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}

	want, _ := wol.MagicPacket(mac)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("Send() packet = %x, want %x", buf[:n], want)
	}
}

func TestSendRejectsInvalidMACAddress(t *testing.T) {
	t.Parallel()

	sender := wol.NewSender("127.0.0.1:9")
	if err := sender.Send("invalid"); err == nil {
		t.Fatal("Send() error = nil, want error")
	}
}

func TestSendFailsOnUnreachableAddress(t *testing.T) {
	t.Parallel()

	// 不正なアドレスへの dial が失敗することを確認する
	sender := wol.NewSender("not-a-valid-address")
	if err := sender.Send("00:11:22:33:44:55"); err == nil {
		t.Fatal("Send() error = nil, want error")
	}
}

func TestMagicPacketRejectsInvalidMACAddress(t *testing.T) {

	tests := []struct {
		name       string
		macAddress string
	}{
		{name: "empty", macAddress: ""},
		{name: "not a mac address", macAddress: "not-a-mac-address"},
		{name: "too short", macAddress: "00:11:22:33:44"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := wol.MagicPacket(tt.macAddress)
			if err == nil {
				t.Fatal("MagicPacket() error = nil, want error")
			}
		})
	}
}
