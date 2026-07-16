// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inventory

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const logicalSectorBytes = 512

type LinuxPaths struct {
	SysClassBlock string
	SysDevBlock   string
	DevDiskByID   string
	MountInfo     string
}

func DefaultLinuxPaths() LinuxPaths {
	return LinuxPaths{
		SysClassBlock: "/sys/class/block",
		SysDevBlock:   "/sys/dev/block",
		DevDiskByID:   "/dev/disk/by-id",
		MountInfo:     "/proc/self/mountinfo",
	}
}

type LinuxCollector struct {
	paths LinuxPaths
}

func NewLinuxCollector(paths LinuxPaths) *LinuxCollector {
	return &LinuxCollector{paths: paths}
}

func (collector *LinuxCollector) Collect() ([]disk.Device, error) {
	entries, err := os.ReadDir(collector.paths.SysClassBlock)
	if err != nil {
		return nil, fmt.Errorf("read block device inventory: %w", err)
	}
	byID, err := readByIDPaths(collector.paths.DevDiskByID)
	if err != nil {
		return nil, err
	}
	agentOSDevices, err := collector.agentOSDevices()
	if err != nil {
		return nil, err
	}

	devices := make([]disk.Device, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !isDiskCandidate(name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(collector.paths.SysClassBlock, name, "partition")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect block device %s: %w", name, err)
		}
		sizeBytes, err := readSizeBytes(filepath.Join(collector.paths.SysClassBlock, name, "size"))
		if err != nil {
			return nil, fmt.Errorf("read block device %s size: %w", name, err)
		}
		devices = append(devices, disk.Device{
			Path:         filepath.Join("/dev", name),
			ByIDPaths:    byID[name],
			SerialNumber: readOptional(filepath.Join(collector.paths.SysClassBlock, name, "device", "serial")),
			WWN:          readFirstOptional(filepath.Join(collector.paths.SysClassBlock, name, "device", "wwid"), filepath.Join(collector.paths.SysClassBlock, name, "wwid")),
			SizeBytes:    sizeBytes,
			HoldsAgentOS: agentOSDevices[name],
		})
	}
	slices.SortFunc(devices, func(left, right disk.Device) int {
		return strings.Compare(left.Path, right.Path)
	})
	return devices, nil
}

func ToProtocol(systemUUID, bootMACAddress string, devices []disk.Device) agentprotocol.Inventory {
	disks := make([]agentprotocol.DiskInventory, 0, len(devices))
	for _, device := range devices {
		disks = append(disks, agentprotocol.DiskInventory{
			DevicePath:   device.Path,
			ByIDPaths:    slices.Clone(device.ByIDPaths),
			SerialNumber: device.SerialNumber,
			WWN:          device.WWN,
			SizeBytes:    device.SizeBytes,
			HoldsAgentOS: device.HoldsAgentOS,
		})
	}
	return agentprotocol.Inventory{
		SystemUUID:     systemUUID,
		BootMACAddress: bootMACAddress,
		Disks:          disks,
	}
}

func readByIDPaths(directory string) (map[string][]string, error) {
	result := map[string][]string{}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read stable disk identities: %w", err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		deviceName := filepath.Base(target)
		result[deviceName] = append(result[deviceName], filepath.Join("/dev/disk/by-id", entry.Name()))
	}
	for name := range result {
		slices.Sort(result[name])
	}
	return result, nil
}

func (collector *LinuxCollector) agentOSDevices() (map[string]bool, error) {
	data, err := os.ReadFile(collector.paths.MountInfo)
	if err != nil {
		return nil, fmt.Errorf("read mount information: %w", err)
	}
	majorMinor, ok := rootMountDevice(string(data))
	if !ok {
		return nil, errors.New("root mount is absent from mountinfo")
	}
	target, err := os.Readlink(filepath.Join(collector.paths.SysDevBlock, majorMinor))
	if errors.Is(err, os.ErrNotExist) {
		// initramfsのrootがtmpfs等の場合、保持するblock deviceは存在しない。
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve root block device: %w", err)
	}
	rootDevice, ok := wholeDiskFromSysTarget(target)
	if !ok {
		return map[string]bool{}, nil
	}
	result := map[string]bool{}
	if err := collector.addDeviceAndSlaves(rootDevice, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (collector *LinuxCollector) addDeviceAndSlaves(name string, result map[string]bool) error {
	if result[name] {
		return nil
	}
	result[name] = true
	slavesDirectory := filepath.Join(collector.paths.SysClassBlock, name, "slaves")
	entries, err := os.ReadDir(slavesDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read root block device slaves: %w", err)
	}
	for _, entry := range entries {
		if err := collector.addDeviceAndSlaves(entry.Name(), result); err != nil {
			return err
		}
	}
	return nil
}

func rootMountDevice(mountInfo string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(mountInfo))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 5 && fields[4] == "/" {
			return fields[2], true
		}
	}
	return "", false
}

func isDiskCandidate(name string) bool {
	for _, prefix := range []string{"loop", "ram", "zram", "fd", "sr"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

func wholeDiskFromSysTarget(target string) (string, bool) {
	parts := strings.Split(filepath.Clean(target), string(filepath.Separator))
	for index, part := range parts {
		if part == "block" && index+1 < len(parts) {
			return parts[index+1], true
		}
	}
	return "", false
}

func readSizeBytes(path string) (int64, error) {
	value := readOptional(path)
	sectors, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse sector count: %w", err)
	}
	if sectors <= 0 || sectors > (1<<63-1)/logicalSectorBytes {
		return 0, errors.New("sector count is outside the supported range")
	}
	return sectors * logicalSectorBytes, nil
}

func readFirstOptional(paths ...string) string {
	for _, path := range paths {
		if value := readOptional(path); value != "" {
			return value
		}
	}
	return ""
}

func readOptional(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
