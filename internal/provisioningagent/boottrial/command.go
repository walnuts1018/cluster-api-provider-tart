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

package boottrial

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

const MaxAttempts = 3

type Request struct {
	BootDevicePath     string
	ActiveSlot         string
	TargetSlot         string
	RollbackSlot       string
	ArtifactGeneration uint64
	MaxAttempts        int
}

type Driver interface {
	Configure(context.Context, Request) error
}

type Runner func(context.Context, string, ...string) ([]byte, error)

type CommandDriver struct {
	path string
	run  Runner
}

func NewCommandDriver(path string, runner Runner) *CommandDriver {
	if runner == nil {
		runner = runCommand
	}
	return &CommandDriver{
		path: path,
		run:  runner,
	}
}

func (driver *CommandDriver) Configure(ctx context.Context, request Request) error {
	if err := ValidateRequest(request); err != nil {
		return err
	}
	output, err := driver.run(ctx, driver.path,
		"configure",
		"--boot-device", request.BootDevicePath,
		"--active-slot", request.ActiveSlot,
		"--target-slot", request.TargetSlot,
		"--rollback-slot", request.RollbackSlot,
		"--artifact-generation", strconv.FormatUint(request.ArtifactGeneration, 10),
		"--max-attempts", strconv.Itoa(request.MaxAttempts),
	)
	if err != nil {
		output = bytes.TrimSpace(output)
		if len(output) == 0 {
			return fmt.Errorf("configure boot trial metadata: %w", err)
		}
		return fmt.Errorf("configure boot trial metadata: %w: %s", err, output)
	}
	return nil
}

func ValidateRequest(request Request) error {
	switch {
	case request.BootDevicePath == "":
		return errors.New("boot device path is required")
	case request.ActiveSlot != "A" && request.ActiveSlot != "B":
		return fmt.Errorf("active slot must be A or B, got %q", request.ActiveSlot)
	case request.TargetSlot != "A" && request.TargetSlot != "B":
		return fmt.Errorf("target slot must be A or B, got %q", request.TargetSlot)
	case request.RollbackSlot != "A" && request.RollbackSlot != "B":
		return fmt.Errorf("rollback slot must be A or B, got %q", request.RollbackSlot)
	case request.ActiveSlot == request.TargetSlot:
		return errors.New("target slot must differ from active slot")
	case request.RollbackSlot != request.ActiveSlot:
		return errors.New("rollback slot must match active slot")
	case request.ArtifactGeneration == 0:
		return errors.New("artifact generation must be greater than zero")
	case request.MaxAttempts <= 0:
		return errors.New("max attempts must be greater than zero")
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
