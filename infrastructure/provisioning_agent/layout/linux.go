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

package layout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type LinuxDiskIO struct{}

func NewLinuxDiskIO() *LinuxDiskIO {
	return &LinuxDiskIO{}
}

func (diskIO *LinuxDiskIO) LogicalSectorSize(ctx context.Context, devicePath string) (int64, error) {
	output, err := exec.CommandContext(ctx, "blockdev", "--getss", devicePath).CombinedOutput()
	if err != nil {
		return 0, commandError("blockdev --getss", output, err)
	}
	sectorSize, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse logical sector size %q: %w", strings.TrimSpace(string(output)), err)
	}
	return sectorSize, nil
}

func (diskIO *LinuxDiskIO) CreatePartitionTable(
	ctx context.Context,
	devicePath string,
	planned PlannedLayout,
) error {
	script, err := renderSFDiskScript(planned)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "sfdisk", "--wipe", "always", "--wipe-partitions", "always", devicePath)
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		return commandError("sfdisk create GPT", output, err)
	}

	// kernelが新しいpartition deviceを公開する前にRole解決へ進まない。
	output, err = exec.CommandContext(ctx, "udevadm", "settle", "--timeout=30").CombinedOutput()
	if err != nil {
		return commandError("udevadm settle", output, err)
	}
	return nil
}

func (diskIO *LinuxDiskIO) ReadPartitionTable(ctx context.Context, devicePath string) (ObservedLayout, error) {
	output, err := exec.CommandContext(ctx, "sfdisk", "--json", devicePath).CombinedOutput()
	if err != nil {
		return ObservedLayout{}, commandError("sfdisk read GPT", output, err)
	}
	observed, err := parseSFDiskJSON(output)
	if err != nil {
		return ObservedLayout{}, err
	}
	return observed, nil
}

type sfdiskOutput struct {
	PartitionTable struct {
		Label      string `json:"label"`
		SectorSize int64  `json:"sectorsize"`
		Partitions []struct {
			Node  string `json:"node"`
			Start int64  `json:"start"`
			Size  int64  `json:"size"`
			Type  string `json:"type"`
			UUID  string `json:"uuid"`
			Name  string `json:"name"`
		} `json:"partitions"`
	} `json:"partitiontable"`
}

func parseSFDiskJSON(data []byte) (ObservedLayout, error) {
	var output sfdiskOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return ObservedLayout{}, fmt.Errorf("decode sfdisk JSON: %w", err)
	}
	partitions := make([]ObservedPartition, 0, len(output.PartitionTable.Partitions))
	for _, partition := range output.PartitionTable.Partitions {
		partitions = append(partitions, ObservedPartition{
			DevicePath:  partition.Node,
			Label:       partition.Name,
			TypeGUID:    partition.Type,
			PARTUUID:    partition.UUID,
			StartSector: partition.Start,
			SizeSectors: partition.Size,
		})
	}
	return ObservedLayout{
		TableType:  output.PartitionTable.Label,
		SectorSize: output.PartitionTable.SectorSize,
		Partitions: partitions,
	}, nil
}

func renderSFDiskScript(planned PlannedLayout) (string, error) {
	if planned.ProfileID != ProfileID {
		return "", fmt.Errorf("unsupported platform profile: %q", planned.ProfileID)
	}
	if planned.SectorSize <= 0 || len(planned.Partitions) != len(profileDefinitions) {
		return "", fmt.Errorf("%w: incomplete partition plan", ErrInvalidLayout)
	}

	var script bytes.Buffer
	script.WriteString("label: gpt\nunit: sectors\n\n")
	for _, partition := range planned.Partitions {
		_, _ = fmt.Fprintf(
			&script,
			"start=%d, size=%d, type=%s, name=%q\n",
			partition.StartSector,
			partition.SizeSectors,
			partition.TypeGUID,
			partition.Label,
		)
	}
	return script.String(), nil
}

func commandError(name string, output []byte, err error) error {
	const maxOutputBytes = 4096
	output = bytes.TrimSpace(output)
	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes]
	}
	if len(output) == 0 {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w: %s", name, err, output)
}
