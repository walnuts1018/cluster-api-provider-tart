package agentapi

import (
	"context"
	"errors"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
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
