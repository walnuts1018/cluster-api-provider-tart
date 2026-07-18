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
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCommandDriverConfigureExecutesTypedArguments(t *testing.T) {
	t.Parallel()

	var gotName string
	var gotArgs []string
	driver := NewCommandDriver("boot-trial-driver", func(
		_ context.Context,
		name string,
		args ...string,
	) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil, nil
	})

	err := driver.Configure(t.Context(), Request{
		BootDevicePath:     "/dev/disk/by-partuuid/boot",
		ActiveSlot:         "A",
		TargetSlot:         "B",
		RollbackSlot:       "A",
		ArtifactGeneration: 12,
		MaxAttempts:        3,
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if gotName != "boot-trial-driver" {
		t.Fatalf("command name = %q, want boot-trial-driver", gotName)
	}
	wantArgs := []string{
		"configure",
		"--boot-device", "/dev/disk/by-partuuid/boot",
		"--active-slot", "A",
		"--target-slot", "B",
		"--rollback-slot", "A",
		"--artifact-generation", "12",
		"--max-attempts", "3",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("command args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestCommandDriverConfigureRejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	driver := NewCommandDriver("boot-trial-driver", func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("command runner should not be called")
		return nil, nil
	})
	if err := driver.Configure(t.Context(), Request{}); err == nil {
		t.Fatal("Configure() error = nil, want invalid request")
	}
}

func TestCommandDriverConfigureReturnsCommandError(t *testing.T) {
	t.Parallel()

	driver := NewCommandDriver("boot-trial-driver", func(context.Context, string, ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("exit status 1")
	})
	err := driver.Configure(t.Context(), Request{
		BootDevicePath:     "/dev/disk/by-partuuid/boot",
		ActiveSlot:         "A",
		TargetSlot:         "B",
		RollbackSlot:       "A",
		ArtifactGeneration: 12,
		MaxAttempts:        3,
	})
	if err == nil {
		t.Fatal("Configure() error = nil, want command failure")
	}
}
