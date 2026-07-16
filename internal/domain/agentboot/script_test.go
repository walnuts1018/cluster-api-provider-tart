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
	"slices"
	"strings"
	"testing"
)

func TestBuildScriptは検証済みArtifactの固定URLを生成する(t *testing.T) {
	script, err := BuildScript(ScriptInput{
		ArtifactBaseURL: "https://boot.test/agent",
		AgentAPIURL:     "https://agent-api.test:8443",
		ArtifactDigest:  "sha256:" + strings.Repeat("a", 64),
		HostUID:         "host-uid",
		OperationUID:    "operation-uid",
		BootMACAddress:  "00:00:5e:00:53:01",
	})
	if err != nil {
		t.Fatalf("BuildScript() error = %v", err)
	}
	for _, expected := range []string{
		"#!ipxe\n",
		"kernel https://boot.test/agent/v1/agent-artifacts/sha256/" + strings.Repeat("a", 64) + "/kernel",
		"initrd=agent-initrd",
		KernelParameterControllerURL + "=https://agent-api.test:8443",
		KernelParameterHostUID + "=host-uid",
		KernelParameterOperationUID + "=operation-uid",
		KernelParameterBootMAC + "=00:00:5e:00:53:01",
		"initrd --name agent-initrd https://boot.test/agent/v1/agent-artifacts/sha256/" + strings.Repeat("a", 64) + "/initrd",
		"\nboot\n",
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("script does not contain %q:\n%s", expected, script)
		}
	}
	for _, secretName := range []string{"token=", "credential=", "authorization="} {
		if strings.Contains(strings.ToLower(script), secretName) {
			t.Errorf("script contains forbidden secret parameter %q:\n%s", secretName, script)
		}
	}
}

func TestBuildScriptは危険な入力を拒否する(t *testing.T) {
	valid := ScriptInput{
		ArtifactBaseURL: "https://boot.test",
		AgentAPIURL:     "https://agent-api.test",
		ArtifactDigest:  "sha256:" + strings.Repeat("a", 64),
		HostUID:         "host-uid",
		OperationUID:    "operation-uid",
		BootMACAddress:  "00:00:5e:00:53:01",
	}
	tests := []struct {
		name   string
		mutate func(*ScriptInput)
	}{
		{name: "平文Agent API URL", mutate: func(input *ScriptInput) { input.AgentAPIURL = "http://agent-api.test" }},
		{name: "不正digest", mutate: func(input *ScriptInput) { input.ArtifactDigest = "sha256:invalid" }},
		{name: "改行を含むHost UID", mutate: func(input *ScriptInput) { input.HostUID = "host\nchain evil" }},
		{name: "不正MAC", mutate: func(input *ScriptInput) { input.BootMACAddress = "invalid" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := BuildScript(input); err == nil {
				t.Fatal("BuildScript() accepted unsafe input")
			}
		})
	}
}

func TestBuildScriptはArtifact配信だけHTTPを許可する(t *testing.T) {
	script, err := BuildScript(ScriptInput{
		ArtifactBaseURL: "http://boot.test/agent",
		AgentAPIURL:     "https://agent-api.test:8443",
		ArtifactDigest:  "sha256:" + strings.Repeat("a", 64),
		HostUID:         "host-uid",
		OperationUID:    "operation-uid",
		BootMACAddress:  "00:00:5e:00:53:01",
	})
	if err != nil {
		t.Fatalf("BuildScript() error = %v", err)
	}
	if !strings.Contains(script, "kernel http://boot.test/agent/v1/agent-artifacts/sha256/") {
		t.Fatalf("script does not contain HTTP Artifact kernel URL:\n%s", script)
	}
	if !strings.Contains(script, KernelParameterControllerURL+"=https://agent-api.test:8443") {
		t.Fatalf("script does not keep HTTPS Agent API URL:\n%s", script)
	}
}

func TestBuildScriptはregister入力をkernel引数へ正規化する(t *testing.T) {
	t.Parallel()

	script, err := BuildScript(ScriptInput{
		ArtifactBaseURL: "https://boot.test/base",
		AgentAPIURL:     "https://controller.test/agent",
		ArtifactDigest:  "sha256:" + strings.Repeat("b", 64),
		HostUID:         "host-uid",
		OperationUID:    "operation-uid",
		BootMACAddress:  "AA-BB-CC-DD-EE-FF",
	})
	if err != nil {
		t.Fatalf("BuildScript() error = %v", err)
	}

	got := extractKernelAgentArgumentsFromIPXEScript(t, script)
	want := []string{
		KernelParameterBootMAC + "=aa:bb:cc:dd:ee:ff",
		KernelParameterControllerURL + "=https://controller.test/agent",
		KernelParameterHostUID + "=host-uid",
		KernelParameterOperationUID + "=operation-uid",
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("kernel args = %v, want %v", got, want)
	}
}

func extractKernelAgentArgumentsFromIPXEScript(t *testing.T, script string) []string {
	t.Helper()

	for line := range strings.SplitSeq(script, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "kernel" {
			continue
		}

		args := make([]string, 0, len(fields)-2)
		for _, field := range fields[2:] {
			if isAgentKernelArgument(field) {
				args = append(args, field)
			}
		}
		return args
	}

	t.Fatal("kernel line not found")
	return nil
}

func isAgentKernelArgument(argument string) bool {
	for _, key := range KernelParameterKeys() {
		if strings.HasPrefix(argument, key+"=") {
			return true
		}
	}
	return false
}
