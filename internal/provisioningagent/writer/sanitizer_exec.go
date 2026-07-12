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

package writer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type LinuxSanitizer struct{}

func NewLinuxSanitizer() *LinuxSanitizer {
	return &LinuxSanitizer{}
}

// Sanitizeはdevice側のsecure discardを優先し、利用できない環境だけzero overwriteへ戻す。
func (sanitizer *LinuxSanitizer) Sanitize(ctx context.Context, devicePath string, _ int64) (bool, error) {
	if _, err := exec.LookPath("blkdiscard"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("find blkdiscard: %w", err)
	}
	output, err := exec.CommandContext(ctx, "blkdiscard", "--secure", devicePath).CombinedOutput()
	if err == nil {
		return true, nil
	}
	if secureDiscardUnsupported(output) {
		return false, nil
	}
	return false, sanitizeCommandError("blkdiscard --secure", output, err)
}

func secureDiscardUnsupported(output []byte) bool {
	message := strings.ToLower(string(bytes.TrimSpace(output)))
	if message == "" {
		return false
	}
	return strings.Contains(message, "not supported") ||
		strings.Contains(message, "operation not supported") ||
		strings.Contains(message, "inappropriate ioctl")
}

func sanitizeCommandError(name string, output []byte, err error) error {
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
