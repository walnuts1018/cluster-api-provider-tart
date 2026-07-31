//go:build e2e

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provisioning

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSimulatorManagerStopsWoLListenerOnContextCancellation(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("空きUDPポートを確保できません: %v", err)
	}
	listenAddress := listener.LocalAddr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("空きUDPポートを解放できません: %v", err)
	}
	listenAddr, err := net.ResolveUDPAddr("udp", listenAddress)
	if err != nil {
		t.Fatalf("UDPアドレスを解決できません: %v", err)
	}

	manager := NewSimulatorManager()
	manager.wolListenAddress = listenAddress
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- manager.Start(ctx)
	}()

	if err := waitForUDPListener(listenAddr, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WoLリスナーの停止に失敗しました: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WoLリスナーがコンテキストのキャンセル後に停止しません")
	}

	verificationListener, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		t.Fatalf("WoLリスナーのポートが解放されません: %v", err)
	}
	defer verificationListener.Close()
}

func TestNewHostSimulatorKeepsSeparateQEMULogs(t *testing.T) {
	first, err := NewHostSimulator("00:00:5e:00:53:00", "br0", "tartroot0")
	if err != nil {
		t.Fatalf("最初のHostSimulatorを作成できません: %v", err)
	}
	second, err := NewHostSimulator("00:00:5e:00:53:00", "br0", "tartroot0")
	if err != nil {
		t.Fatalf("2つ目のHostSimulatorを作成できません: %v", err)
	}
	if first.logFilePath() == second.logFilePath() {
		t.Fatalf("QEMUログのパスが重複しています: %q", first.logFilePath())
	}
}

func waitForUDPListener(listenAddr *net.UDPAddr, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		listener, err := net.ListenUDP("udp", listenAddr)
		if err != nil {
			return nil
		}
		if err := listener.Close(); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return context.DeadlineExceeded
}
