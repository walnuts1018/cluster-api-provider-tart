//go:build e2e
// +build e2e

package provisioning

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type HostSimulator struct {
	macAddress string
	bridge     string
	qemuCmd    *exec.Cmd
	mu         sync.Mutex
}

func NewHostSimulator(macAddress, bridge string) *HostSimulator {
	return &HostSimulator{
		macAddress: macAddress,
		bridge:     bridge,
	}
}

// Start listens for WoL packets on UDP port 9 and starts QEMU when the matching MAC is found.
func (s *HostSimulator) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("simulator")

	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:9")
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}

	logger.Info("Listening for WoL packets", "port", 9, "mac", s.macAddress)

	go func() {
		<-ctx.Done()
		_ = conn.Close()
		s.Stop()
	}()

	buf := make([]byte, 1024)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			logger.Error(err, "Failed to read UDP packet")
			continue
		}

		if s.isWoLPacketForMAC(buf[:n], s.macAddress) {
			logger.Info("Received WoL packet, starting QEMU", "mac", s.macAddress)
			if err := s.startQEMU(ctx); err != nil {
				logger.Error(err, "Failed to start QEMU")
			}
		}
	}
}

func (s *HostSimulator) isWoLPacketForMAC(packet []byte, targetMAC string) bool {
	if len(packet) < 102 {
		return false
	}

	mac, err := net.ParseMAC(targetMAC)
	if err != nil {
		return false
	}

	// Check for magic sequence: 6 bytes of 0xFF followed by 16 repetitions of MAC
	for i := range 6 {
		if packet[i] != 0xFF {
			return false
		}
	}

	offset := 6
	for i := range 16 {
		for j := range 6 {
			if packet[offset+i*6+j] != mac[j] {
				return false
			}
		}
	}

	return true
}

func (s *HostSimulator) startQEMU(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.qemuCmd != nil && s.qemuCmd.Process != nil {
		// Already running
		return nil
	}

	logger := log.FromContext(ctx).WithName("qemu")

	logFile := fmt.Sprintf("qemu-output-%s.log", hex.EncodeToString([]byte(s.macAddress)))
	args := []string{
		"-enable-kvm",
		"-m", "2048",
		"-smp", "2",
		"-boot", "n",
		"-netdev", fmt.Sprintf("bridge,br=%s,id=net0", s.bridge),
		"-device", fmt.Sprintf("virtio-net-pci,netdev=net0,mac=%s", s.macAddress),
		"-bios", "/usr/share/ovmf/OVMF.fd",
		"-nographic",
		"-serial", fmt.Sprintf("file:%s", logFile),
		"-display", "none",
	}

	cmd := exec.Command("sudo", append([]string{"qemu-system-x86_64"}, args...)...)

	// Create qemu log file and ensure we can write to it
	if f, err := os.Create(logFile); err == nil {
		_ = f.Close()
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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

func (s *HostSimulator) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.qemuCmd != nil && s.qemuCmd.Process != nil {
		// Try graceful shutdown via monitor or just kill.
		// Since it's sudo, we need to sudo kill.
		_ = exec.Command("sudo", "kill", fmt.Sprintf("%d", s.qemuCmd.Process.Pid)).Run()

		// Force kill if it doesn't die
		go func(pid int) {
			time.Sleep(5 * time.Second)
			_ = exec.Command("sudo", "kill", "-9", fmt.Sprintf("%d", pid)).Run()
		}(s.qemuCmd.Process.Pid)

		s.qemuCmd = nil
	}
}
