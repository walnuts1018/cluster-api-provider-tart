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
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

var simulatorLogSequence atomic.Uint64

// SimulatorManager manages multiple HostSimulators and a single UDP listener.
type SimulatorManager struct {
	simulators       map[string]*HostSimulator
	wolListenAddress string
	mu               sync.RWMutex
}

func NewSimulatorManager() *SimulatorManager {
	return &SimulatorManager{
		simulators:       make(map[string]*HostSimulator),
		wolListenAddress: "0.0.0.0:9",
	}
}

func (m *SimulatorManager) Register(s *HostSimulator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.simulators[s.macAddress] = s
}

func (m *SimulatorManager) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("simulator-manager")

	addr, err := net.ResolveUDPAddr("udp", m.wolListenAddress)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Error(err, "Failed to close WoL listener")
		}
	}()

	logger.Info("Listening for WoL packets on port 9")

	go func() {
		<-ctx.Done()
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Error(err, "Failed to close WoL listener after cancellation")
		}
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
	macAddress      string
	macAddressBytes []byte
	bridge          string
	diskSerial      string
	diskPath        string
	qemuCmd         *exec.Cmd
	qemuDone        chan struct{}
	logFile         string
	stopping        bool
	mu              sync.Mutex
}

func NewHostSimulator(macAddress, bridge, diskSerial string) (*HostSimulator, error) {
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MAC address %s: %w", macAddress, err)
	}

	return &HostSimulator{
		macAddress:      macAddress,
		macAddressBytes: mac,
		bridge:          bridge,
		diskSerial:      diskSerial,
		logFile:         fmt.Sprintf("qemu-output-%s-%d.log", hex.EncodeToString([]byte(macAddress)), simulatorLogSequence.Add(1)),
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

	if s.stopping {
		return nil
	}
	if s.qemuCmd != nil && s.qemuCmd.Process != nil {
		return nil
	}

	logger := log.FromContext(ctx).WithName("qemu").WithValues("mac", s.macAddress)

	ovmfPath := s.findOVMF()
	if ovmfPath == "" {
		return fmt.Errorf("failed to find OVMF.fd")
	}

	diskPath, err := s.ensureRootDisk()
	if err != nil {
		return err
	}

	logFile := s.logFilePath()
	args := []string{
		"-enable-kvm",
		"-m", "2048",
		"-smp", "2",
		"-boot", "n",
		"-netdev", fmt.Sprintf("bridge,br=%s,id=net0", s.bridge),
		"-device", fmt.Sprintf("virtio-net-pci,netdev=net0,mac=%s", s.macAddress),
		"-drive", fmt.Sprintf("file=%s,if=none,id=rootdisk,format=qcow2", diskPath),
		"-device", fmt.Sprintf("virtio-blk-pci,drive=rootdisk,serial=%s", s.diskSerial),
		"-bios", ovmfPath,
		"-nographic",
		"-serial", fmt.Sprintf("file:%s", logFile),
		"-display", "none",
	}

	cmd := exec.Command("sudo", append([]string{"qemu-system-x86_64"}, args...)...)

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
	done := make(chan struct{})
	s.qemuDone = done
	logger.Info("QEMU started", "pid", cmd.Process.Pid)

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		if s.qemuCmd == cmd {
			s.qemuCmd = nil
			s.qemuDone = nil
		}
		s.mu.Unlock()
		close(done)
		if err != nil {
			logger.Error(err, "QEMU process exited with error")
		} else {
			logger.Info("QEMU process exited cleanly")
		}
	}()

	return nil
}

func (s *HostSimulator) LogContainsAll(values ...string) (bool, string, error) {
	data, err := os.ReadFile(s.logFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}

	logText := string(data)
	for _, value := range values {
		if !strings.Contains(logText, value) {
			return false, logText, nil
		}
	}

	return true, logText, nil
}

func (s *HostSimulator) logFilePath() string {
	return s.logFile
}

func (s *HostSimulator) ensureRootDisk() (string, error) {
	if s.diskPath != "" {
		return s.diskPath, nil
	}

	diskFile, err := os.CreateTemp("", "tart-e2e-root-*.qcow2")
	if err != nil {
		return "", fmt.Errorf("failed to create root disk path: %w", err)
	}
	diskPath := diskFile.Name()
	if err := diskFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close root disk file: %w", err)
	}
	if err := os.Remove(diskPath); err != nil {
		return "", fmt.Errorf("failed to prepare root disk file: %w", err)
	}

	if err := exec.Command("qemu-img", "create", "-f", "qcow2", diskPath, "80G").Run(); err != nil {
		return "", fmt.Errorf("failed to create QEMU root disk: %w", err)
	}

	s.diskPath = diskPath
	return diskPath, nil
}

func (s *HostSimulator) findOVMF() string {
	paths := []string{
		"/usr/share/ovmf/OVMF.fd",         // Ubuntu/macOS Brew
		"/usr/share/OVMF/OVMF.fd",         // Fedora/CentOS
		"/usr/share/qemu/ovmf-x86_64.bin", // Arch Linux
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (s *HostSimulator) Stop() error {
	s.mu.Lock()
	s.stopping = true
	cmd := s.qemuCmd
	done := s.qemuDone
	diskPath := s.diskPath
	s.mu.Unlock()

	if cmd != nil && cmd.Process != nil && done != nil {
		pid := cmd.Process.Pid
		stopQEMU := func(signal string) {
			_ = exec.Command("sudo", "pkill", signal, "-P", fmt.Sprintf("%d", pid)).Run()
			_ = exec.Command("sudo", "kill", signal, fmt.Sprintf("%d", pid)).Run()
		}
		stopQEMU("-TERM")
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			stopQEMU("-KILL")
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				return fmt.Errorf("QEMU process %d did not exit after KILL", pid)
			}
		}
	}
	if diskPath != "" {
		if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove root disk %s: %w", diskPath, err)
		}
		s.mu.Lock()
		if s.diskPath == diskPath {
			s.diskPath = ""
		}
		s.mu.Unlock()
	}
	return nil
}
