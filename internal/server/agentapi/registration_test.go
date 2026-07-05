package agentapi

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestIsolatedL2RegistrationVerifier(t *testing.T) {
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "operation"},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			HostRef: infrastructurev1beta1.ResourceReference{
				UID: types.UID("host-uid"),
			},
		},
	}
	request := agentprotocol.RegisterRequest{
		APIVersion:      agentprotocol.APIVersion,
		OperationUID:    "operation-uid",
		HostUID:         "host-uid",
		AgentInstanceID: "agent-instance",
		Inventory: agentprotocol.Inventory{
			Disks: []agentprotocol.DiskInventory{{
				DevicePath: "/dev/disk/by-id/test",
				ByIDPaths:  []string{"/dev/disk/by-id/test"},
				SizeBytes:  64 << 30,
			}},
		},
	}
	verifier := IsolatedL2RegistrationVerifier{}
	if err := verifier.Verify(t.Context(), operation, "", request); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	request.HostUID = "other-host"
	if err := verifier.Verify(t.Context(), operation, "", request); err == nil {
		t.Fatal("Verify() accepted another Host UID")
	}
}
