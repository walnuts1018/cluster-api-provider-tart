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

func TestParseKernelParametersは互換aliasを含む契約を1か所で解決する(t *testing.T) {
	t.Parallel()

	params := ParseKernelParameters(stringsJoinFields(
		KernelParameterControllerURL+"=https://controller.test/agent",
		KernelParameterHostUID+"=host-uid",
		KernelParameterOperationUID+"=operation-uid",
		legacyKernelParameterBootMAC+"=AA-BB-CC-DD-EE-FF",
		KernelParameterTrustURL+"=http://boot.test/agent",
	))
	if params.ControllerURL != "https://controller.test/agent" ||
		params.HostUID != "host-uid" ||
		params.OperationUID != "operation-uid" ||
		params.BootMAC != "AA-BB-CC-DD-EE-FF" ||
		params.TrustURL != "http://boot.test/agent" {
		t.Fatalf("ParseKernelParameters() = %#v", params)
	}

	keys := KernelParameterKeys()
	slices.Sort(keys)
	wantKeys := []string{
		KernelParameterBootMAC,
		KernelParameterControllerURL,
		KernelParameterHostUID,
		KernelParameterOperationUID,
		KernelParameterTrustURL,
	}
	slices.Sort(wantKeys)
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("KernelParameterKeys() = %v, want %v", keys, wantKeys)
	}
}

func stringsJoinFields(fields ...string) string {
	return strings.Join(fields, " ")
}
