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

package agentboot

import (
	"fmt"
	"net"
	"strings"
)

const (
	KernelParameterControllerURL = "tart.agent.controller-url"
	KernelParameterHostUID       = "tart.agent.host-uid"
	KernelParameterOperationUID  = "tart.agent.operation-uid"
	KernelParameterBootMAC       = "tart.agent.boot-mac"
	legacyKernelParameterBootMAC = "tart.agent.boot-mac-address"
)

type KernelParameters struct {
	ControllerURL string
	HostUID       string
	OperationUID  string
	BootMAC       string
}

type kernelParameterDefinition struct {
	canonical string
	aliases   []string
	set       func(*KernelParameters, string)
}

var kernelParameterDefinitions = []kernelParameterDefinition{
	{
		canonical: KernelParameterControllerURL,
		set: func(params *KernelParameters, value string) {
			params.ControllerURL = value
		},
	},
	{
		canonical: KernelParameterHostUID,
		set: func(params *KernelParameters, value string) {
			params.HostUID = value
		},
	},
	{
		canonical: KernelParameterOperationUID,
		set: func(params *KernelParameters, value string) {
			params.OperationUID = value
		},
	},
	{
		canonical: KernelParameterBootMAC,
		aliases:   []string{legacyKernelParameterBootMAC},
		set: func(params *KernelParameters, value string) {
			params.BootMAC = value
		},
	},
}

func (params KernelParameters) Arguments() ([]string, error) {
	controllerURL, err := parseHTTPSBaseURL("Agent API", params.ControllerURL)
	if err != nil {
		return nil, err
	}
	if !identifierPattern.MatchString(params.HostUID) {
		return nil, fmt.Errorf("%s is invalid", KernelParameterHostUID)
	}
	if !identifierPattern.MatchString(params.OperationUID) {
		return nil, fmt.Errorf("%s is invalid", KernelParameterOperationUID)
	}
	mac, err := net.ParseMAC(params.BootMAC)
	if err != nil {
		return nil, fmt.Errorf("%s is invalid", KernelParameterBootMAC)
	}

	return []string{
		KernelParameterControllerURL + "=" + controllerURL.String(),
		KernelParameterHostUID + "=" + params.HostUID,
		KernelParameterOperationUID + "=" + params.OperationUID,
		KernelParameterBootMAC + "=" + mac.String(),
	}, nil
}

func ParseKernelParameters(line string) KernelParameters {
	values := map[string]string{}
	for field := range strings.FieldsSeq(line) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" || value == "" {
			continue
		}
		values[key] = value
	}

	var params KernelParameters
	for _, definition := range kernelParameterDefinitions {
		for _, key := range definition.keys() {
			value, ok := values[key]
			if !ok {
				continue
			}
			definition.set(&params, value)
		}
	}
	return params
}

func KernelParameterKeys() []string {
	keys := make([]string, 0, len(kernelParameterDefinitions))
	for _, definition := range kernelParameterDefinitions {
		keys = append(keys, definition.canonical)
	}
	return keys
}

func (definition kernelParameterDefinition) keys() []string {
	keys := make([]string, 0, 1+len(definition.aliases))
	keys = append(keys, definition.canonical)
	keys = append(keys, definition.aliases...)
	return keys
}
