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
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/opencontainers/go-digest"
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9_.:]{0,127}$`)

type ScriptInput struct {
	ArtifactBaseURL string
	AgentAPIURL     string
	ArtifactDigest  string
	HostUID         string
	OperationUID    string
	BootMACAddress  string
}

func BuildScript(input ScriptInput) (string, error) {
	artifactBaseURL, err := parseArtifactBaseURL(input.ArtifactBaseURL)
	if err != nil {
		return "", err
	}
	agentAPIURL, err := parseHTTPSBaseURL("Agent API", input.AgentAPIURL)
	if err != nil {
		return "", err
	}
	artifactDigest := digest.Digest(input.ArtifactDigest)
	if err := artifactDigest.Validate(); err != nil || artifactDigest.Algorithm() != digest.SHA256 {
		return "", errors.New("agent Artifact digest must be a canonical SHA-256 digest")
	}
	if !identifierPattern.MatchString(input.HostUID) {
		return "", errors.New("host UID is invalid")
	}
	if !identifierPattern.MatchString(input.OperationUID) {
		return "", errors.New("operation UID is invalid")
	}

	artifactPath := fmt.Sprintf("v1/agent-artifacts/sha256/%s", artifactDigest.Encoded())
	kernelURL, err := url.JoinPath(artifactBaseURL.String(), artifactPath, "kernel")
	if err != nil {
		return "", fmt.Errorf("build Agent kernel URL: %w", err)
	}
	initrdURL, err := url.JoinPath(artifactBaseURL.String(), artifactPath, "initrd")
	if err != nil {
		return "", fmt.Errorf("build Agent initrd URL: %w", err)
	}

	params, err := KernelParameters{
		ControllerURL: agentAPIURL.String(),
		HostUID:       input.HostUID,
		OperationUID:  input.OperationUID,
		BootMAC:       input.BootMACAddress,
	}.Arguments()
	if err != nil {
		if strings.Contains(err.Error(), KernelParameterBootMAC) {
			return "", errors.New("boot MAC address is invalid")
		}
		return "", err
	}
	params = append([]string{"initrd=agent-initrd", "console=ttyS0", "ip=dhcp"}, params...)
	return fmt.Sprintf(
		"#!ipxe\nkernel %s %s\ninitrd --name agent-initrd %s\nboot\n",
		kernelURL,
		strings.Join(params, " "),
		initrdURL,
	), nil
}

func parseArtifactBaseURL(value string) (*url.URL, error) {
	parsed, err := parseBaseURL("Artifact", value)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, errors.New("artifact base URL must be an HTTP(S) origin or base path")
	}
	return parsed, nil
}

func parseHTTPSBaseURL(name, value string) (*url.URL, error) {
	parsed, err := parseBaseURL(name, value)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s base URL must be an HTTPS origin or base path", name)
	}
	return parsed, nil
}

func parseBaseURL(name, value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("parse %s base URL: %w", name, err)
	}
	if parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s base URL must be an origin or base path", name)
	}
	return parsed, nil
}
