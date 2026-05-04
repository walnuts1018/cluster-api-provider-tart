//go:build e2e
// +build e2e

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
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SimulatorManager manages multiple HostSimulators and a single UDP listener.
type SimulatorManager struct {
	simulators map[string]*HostSimulator
	mu         sync.RWMutex
}

func NewSimulatorManager() *SimulatorManager {
	return &SimulatorManager{
		simulators: make(map[string]*HostSimulator),
	}
}

func (m *SimulatorManager) Register(s *HostSimulator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.simulators[s.macAddress] = s
}

func (m *SimulatorManager) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("simulator-manager")

	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:9")
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	logger.Info("Listening for WoL packets on port 9")

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 1024)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Error(err, "Failed to read UDP packet")
			continue
		}

		m.dispatch(ctx, buf[:n])
	}
}

func (m *SimulatorManager) dispatch(ctx context.Context, packet []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.simulators {
		if s.isWoLPacketForMAC(packet) {
			logger := log.FromContext(ctx).WithName("simulator-manager")
			logger.Info("Received WoL packet, starting simulator", "mac", s.macAddress)
			if err := s.Start(ctx); err != nil {
				logger.Error(err, "Failed to start simulator", "mac", s.macAddress)
			}
		}
	}
}

type HostSimulator struct {
	macAddress     string
	macAddressBytes []byte
	bridge         string
	qemuCmd        *exec.Cmd
	mu             sync.Mutex
}

func NewHostSimulator(macAddress, bridge string) (*HostSimulator, error) {
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MAC address %s: %w", macAddress, err)
	}

	return &HostSimulator{
		macAddress:     macAddress,
		macAddressBytes: mac,
		bridge:         bridge,
	}, nil
}

func (s *HostSimulator) isWoLPacketForMAC(packet []byte) bool {
	if len(packet) < 102 {
		return false
	}

	// Check for magic sequence: 6 bytes of 0xFF followed by 16 repetitions of MAC
	for i := 0; i < 6; i++ {
		if packet[i] != 0xFF {
			return false
		}
	}

	offset := 6
	for i := 0; i < 16; i++ {
		for j := 0; j < 6; j++ {
			if packet[offset+i*6+j] != s.macAddressBytes[j] {
				return false
			}
		}
	}

	return true
}

func (s *HostSimulator) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.qemuCmd != nil && s.qemuCmd.Process != nil {
		// Already running
		return nil
	}

	logger := log.FromContext(ctx).WithName("qemu").WithValues("mac", s.macAddress)

	ovmfPath := s.findOVMF()
	if ovmfPath == "" {
		return fmt.Errorf("failed to find OVMF.fd")
	}

	logFile := fmt.Sprintf("qemu-output-%s.log", hex.EncodeToString([]byte(s.macAddress)))
	args := []string{
		"-enable-kvm",
		"-m", "2048",
		"-smp", "2",
		"-boot", "n",
		"-netdev", fmt.Sprintf("bridge,br=%s,id=net0", s.bridge),
		"-device", fmt.Sprintf("virtio-net-pci,netdev=net0,mac=%s", s.macAddress),
		"-bios", ovmfPath,
		"-nographic",
		"-serial", fmt.Sprintf("file:%s", logFile),
		"-display", "none",
	}

	cmd := exec.Command("sudo", append([]string{"qemu-system-x86_64"}, args...)...)
	// Use a process group so we can kill sudo and all its children
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create qemu log file and ensure we can write to it
	if f, err := os.Create(logFile); err == nil {
		_ = f.Close()
	}

	// Use io.Discard to avoid flooding test output, as logs are captured in logFile
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return err
	}

	s.qemuCmd = cmd
	logger.Info("QEMU started", "pid", cmd.Process.Pid)

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.qemuCmd = nil
		s.mu.Unlock()
		if err != nil {
			logger.Error(err, "QEMU process exited with error")
		} else {
			logger.Info("QEMU process exited cleanly")
		}
	}()

	return nil
}

func (s *HostSimulator) findOVMF() string {
	paths := []string{
		"/usr/share/ovmf/OVMF.fd",      // Ubuntu/macOS Brew
		"/usr/share/OVMF/OVMF.fd",      // Fedora/CentOS
		"/usr/share/qemu/ovmf-x86_64.bin", // Arch Linux
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (s *HostSimulator) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.qemuCmd != nil && s.qemuCmd.Process != nil {
		pid := s.qemuCmd.Process.Pid
		// Kill the process group (notice the minus sign)
		// Since we used sudo, we need to sudo kill
		err := exec.Command("sudo", "kill", "-TERM", fmt.Sprintf("-%d", pid)).Run()
		if err != nil {
			fmt.Printf("failed to kill process group %d: %v\n", pid, err)
		}

		// Force kill after a timeout if still running
		go func(pgid int) {
			time.Sleep(5 * time.Second)
			_ = exec.Command("sudo", "kill", "-KILL", fmt.Sprintf("-%d", pgid)).Run()
		}(pid)

		s.qemuCmd = nil
	}
}
