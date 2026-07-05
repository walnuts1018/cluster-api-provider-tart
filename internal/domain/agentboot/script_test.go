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
		"tart.agent.controller-url=https://agent-api.test:8443",
		"tart.agent.host-uid=host-uid",
		"tart.agent.operation-uid=operation-uid",
		"tart.agent.boot-mac=00:00:5e:00:53:01",
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
		{name: "平文Artifact URL", mutate: func(input *ScriptInput) { input.ArtifactBaseURL = "http://boot.test" }},
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
