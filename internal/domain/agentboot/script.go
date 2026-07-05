package agentboot

import (
	"errors"
	"fmt"
	"net"
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
	artifactBaseURL, err := parseHTTPSBaseURL("Artifact", input.ArtifactBaseURL)
	if err != nil {
		return "", err
	}
	agentAPIURL, err := parseHTTPSBaseURL("Agent API", input.AgentAPIURL)
	if err != nil {
		return "", err
	}
	artifactDigest := digest.Digest(input.ArtifactDigest)
	if err := artifactDigest.Validate(); err != nil || artifactDigest.Algorithm() != digest.SHA256 {
		return "", errors.New("Agent Artifact digest must be a canonical SHA-256 digest")
	}
	if !identifierPattern.MatchString(input.HostUID) {
		return "", errors.New("Host UID is invalid")
	}
	if !identifierPattern.MatchString(input.OperationUID) {
		return "", errors.New("Operation UID is invalid")
	}
	mac, err := net.ParseMAC(input.BootMACAddress)
	if err != nil {
		return "", errors.New("boot MAC address is invalid")
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

	params := []string{
		"initrd=agent-initrd",
		"tart.agent.controller-url=" + agentAPIURL.String(),
		"tart.agent.host-uid=" + input.HostUID,
		"tart.agent.operation-uid=" + input.OperationUID,
		"tart.agent.boot-mac=" + mac.String(),
	}
	return fmt.Sprintf(
		"#!ipxe\nkernel %s %s\ninitrd --name agent-initrd %s\nboot\n",
		kernelURL,
		strings.Join(params, " "),
		initrdURL,
	), nil
}

func parseHTTPSBaseURL(name, value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("parse %s base URL: %w", name, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s base URL must be an HTTPS origin or base path", name)
	}
	return parsed, nil
}
