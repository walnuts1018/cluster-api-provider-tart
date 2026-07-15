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
	"strings"
)

const defaultKernelCommandLinePath = "/proc/cmdline"

var readKernelCommandLine = func() ([]byte, error) {
	return os.ReadFile(defaultKernelCommandLinePath)
}

var kernelParameterPrefixes = []string{
	"tart.agent.",
}

type kernelParameterBinding struct {
	suffixes []string
	apply    func(*config, string)
}

var kernelParameterBindings = []kernelParameterBinding{
	{
		suffixes: []string{"controller-url"},
		apply: func(cfg *config, value string) {
			cfg.controllerURL = value
		},
	},
	{
		suffixes: []string{"host-uid"},
		apply: func(cfg *config, value string) {
			cfg.hostUID = value
		},
	},
	{
		suffixes: []string{"operation-uid"},
		apply: func(cfg *config, value string) {
			cfg.operationUID = value
		},
	},
	{
		suffixes: []string{"boot-mac", "boot-mac-address"},
		apply: func(cfg *config, value string) {
			cfg.bootMAC = value
		},
	},
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
	var cfg config
	values := map[string]string{}
	for field := range strings.FieldsSeq(line) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" || value == "" {
			continue
		}
		values[key] = value
	}
	for _, binding := range kernelParameterBindings {
		for _, key := range binding.keys() {
			value, ok := values[key]
			if !ok {
				continue
			}
			binding.apply(&cfg, value)
		}
	}
	return cfg
}

func (binding kernelParameterBinding) keys() []string {
	keys := make([]string, 0, len(binding.suffixes)*len(kernelParameterPrefixes))
	for _, prefix := range kernelParameterPrefixes {
		for _, suffix := range binding.suffixes {
			keys = append(keys, prefix+suffix)
		}
	}
	return keys
}
