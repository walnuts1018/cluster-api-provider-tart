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

package main

import (
	"errors"
	"fmt"
	"os"

	agentboot "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentboot"
)

const defaultKernelCommandLinePath = "/proc/cmdline"

var readKernelCommandLine = func() ([]byte, error) {
	return os.ReadFile(defaultKernelCommandLinePath)
}

func loadKernelCommandLineConfig() (config, error) {
	line, err := readKernelCommandLine()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config{}, nil
		}
		return config{}, fmt.Errorf("read kernel command line: %w", err)
	}
	return parseKernelCommandLine(string(line)), nil
}

func parseKernelCommandLine(line string) config {
	params := agentboot.ParseKernelParameters(line)
	return config{
		controllerURL: params.ControllerURL,
		hostUID:       params.HostUID,
		operationUID:  params.OperationUID,
		bootMAC:       params.BootMAC,
	}
}
