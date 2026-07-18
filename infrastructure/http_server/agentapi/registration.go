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

package agentapi

import (
	"context"
	"errors"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

var ErrRegistrationRejected = errors.New("agent registration rejected")

// IsolatedL2RegistrationVerifierはhardware identityを持たないHost向けの限定的な方式である。
// Host真正性は提供せず、隔離L2、TLS証明書pinning、外部から到達不能なlistenerを前提とする。
type IsolatedL2RegistrationVerifier struct{}

func (IsolatedL2RegistrationVerifier) Verify(
	_ context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	authorization string,
	request agentprotocol.RegisterRequest,
) error {
	if authorization != "" ||
		request.HostUID != string(operation.Spec.HostRef.UID) ||
		request.OperationUID != operation.Spec.OperationID ||
		request.AgentInstanceID == "" ||
		len(request.Inventory.Disks) == 0 {
		return ErrRegistrationRejected
	}
	for _, disk := range request.Inventory.Disks {
		if disk.DevicePath == "" || disk.SizeBytes <= 0 {
			return ErrRegistrationRejected
		}
	}
	return nil
}
