//go:build e2e
// +build e2e

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// DumpClusterState collects comprehensive debug information from the cluster
// and writes it to files in the specified artifacts directory.
func DumpClusterState(artifactDir string) error {
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	dumps := []struct {
		filename string
		title    string
		cmd      string
		args     []string
	}{
		{"cluster-resources.txt", "Cluster Resources", "kubectl", []string{"get", "all", "--all-namespaces", "-o", "wide"}},
		{"crds.txt", "Custom Resource Definitions", "kubectl", []string{"get", "crds"}},
		{"controllers.txt", "Controller Deployments", "kubectl", []string{"get", "deployments", "-n", "cluster-api-provider-tart-system", "-o", "yaml"}},
		{"pods.txt", "All Pods", "kubectl", []string{"get", "pods", "--all-namespaces", "-o", "wide"}},
		{"events.txt", "Recent Events", "kubectl", []string{"get", "events", "--all-namespaces", "--sort-by=.lastTimestamp", "-o", "yaml"}},
		{"cluster-api.txt", "Cluster API Resources", "kubectl", []string{"get", "clusters,clustersets,machines,machineclasses,machinedeployments,machinepools,machinehealthchecksets,machinehealthchecks,kubeadmcontrolplanes,kubeadmconfigconfigs,kubeadmconfigtemplates,tartclusters,tartmachines,tartmachinetemplates", "--all-namespaces", "-o", "yaml"}},
		{"services.txt", "Services", "kubectl", []string{"get", "services", "--all-namespaces", "-o", "wide"}},
		{"networkpolicies.txt", "NetworkPolicies", "kubectl", []string{"get", "networkpolicies", "--all-namespaces", "-o", "wide"}},
		{"ipaddressclaims.txt", "IPAddressClaims (Cluster API)", "kubectl", []string{"get", "ipaddressclaims", "--all-namespaces", "-o", "wide"}},
		{"ipaddresspools.txt", "IPAddressPools (Cluster API)", "kubectl", []string{"get", "ipaddresspools", "--all-namespaces", "-o", "wide"}},
	}

	for _, dump := range dumps {
		if err := dumpResource(dump.filename, dump.title, dump.cmd, dump.args, artifactDir); err != nil {
			fmt.Printf("Warning: failed to dump %s: %v\n", dump.title, err)
		}
	}

	return nil
}

// DumpControllerLogs collects logs from all controller-manager pods
// and writes them to a file in the specified artifacts directory.
func DumpControllerLogs(artifactDir string) error {
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	// Get controller pod names
	cmd := exec.Command("kubectl", "get", "pods", "-l", "control-plane=controller-manager",
		"-o", "go-template={{ range .items }}{{ .metadata.name }}{{ \"\\n\" }}{{ end }}",
		"-n", "cluster-api-provider-tart-system")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get controller pod names: %w: %s", err, string(output))
	}

	podNames := GetNonEmptyLines(string(output))
	if len(podNames) == 0 {
		fmt.Println("No controller-manager pods found")
		return nil
	}

	for _, podName := range podNames {
		logFile := filepath.Join(artifactDir, fmt.Sprintf("controller-logs-%s.log", podName))

		// Get current container logs
		cmd = exec.Command("kubectl", "logs", podName, "-n", "cluster-api-provider-tart-system", "--tail=500")
		logOutput, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: failed to get logs for pod %s: %v\n", podName, err)
		} else {
			if err := os.WriteFile(logFile, logOutput, 0o644); err != nil {
				fmt.Printf("Warning: failed to write logs for pod %s: %v\n", podName, err)
			}
		}

		// Get previous container logs (if container crashed/restarted)
		prevLogFile := filepath.Join(artifactDir, fmt.Sprintf("controller-logs-%s-previous.log", podName))
		cmd = exec.Command("kubectl", "logs", podName, "-n", "cluster-api-provider-tart-system",
			"--previous", "--tail=500")
		prevLogOutput, err := cmd.CombinedOutput()
		if err == nil && len(prevLogOutput) > 0 {
			if err := os.WriteFile(prevLogFile, prevLogOutput, 0o644); err != nil {
				fmt.Printf("Warning: failed to write previous logs for pod %s: %v\n", podName, err)
			}
		}

		// Get pod description
		descFile := filepath.Join(artifactDir, fmt.Sprintf("pod-description-%s.txt", podName))
		cmd = exec.Command("kubectl", "describe", "pod", podName, "-n", "cluster-api-provider-tart-system")
		descOutput, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: failed to describe pod %s: %v\n", podName, err)
		} else {
			if err := os.WriteFile(descFile, descOutput, 0o644); err != nil {
				fmt.Printf("Warning: failed to write pod description for %s: %v\n", podName, err)
			}
		}

		// Get pod events
		eventsFile := filepath.Join(artifactDir, fmt.Sprintf("pod-events-%s.txt", podName))
		cmd = exec.Command("kubectl", "get", "events", "--field-selector", fmt.Sprintf("involvedObject.name=%s", podName),
			"-n", "cluster-api-provider-tart-system", "--sort-by=.lastTimestamp")
		eventsOutput, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: failed to get events for pod %s: %v\n", podName, err)
		} else {
			if err := os.WriteFile(eventsFile, eventsOutput, 0o644); err != nil {
				fmt.Printf("Warning: failed to write events for pod %s: %v\n", podName, err)
			}
		}
	}

	return nil
}

// DumpDnsmasqState collects dnsmasq-related debug information.
func DumpDnsmasqState(artifactDir string) error {
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	dumps := []struct {
		filename string
		cmd      string
		args     []string
	}{
		{"dnsmasq-process.txt", "pgrep", []string{"-af", "dnsmasq"}},
		{"dnsmasq-leases.txt", "cat", []string{"/tmp/dnsmasq.leases"}},
		{"dnsmasq-dhcp.txt", "dnsmasq", []string{"--query=.", "--log-dhcp", "--log-queries"}},
		{"bridge-info.txt", "ip", []string{"link", "show", "br0"}},
		{"bridge-addresses.txt", "ip", []string{"addr", "show", "br0"}},
		{"route-table.txt", "ip", []string{"route"}},
		{"iptables-nat.txt", "sudo", []string{"iptables", "-t", "nat", "-L", "-n", "-v"}},
		{"iptables-filter.txt", "sudo", []string{"iptables", "-L", "-n", "-v"}},
		{"qemu-status.txt", "pgrep", []string{"-af", "qemu"}},
		{"network-interfaces.txt", "ip", []string{"addr"}},
	}

	for _, dump := range dumps {
		if err := dumpResource(dump.filename, dump.filename, dump.cmd, dump.args, artifactDir); err != nil {
			fmt.Printf("Warning: failed to dump %s: %v\n", dump.filename, err)
		}
	}

	return nil
}

// dumpResource executes a kubectl command and writes the output to a file.
func dumpResource(filename, title, cmd string, args []string, artifactDir string) error {
	output, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", title, err, string(output))
	}

	filePath := filepath.Join(artifactDir, filename)
	return os.WriteFile(filePath, output, 0o644)
}
