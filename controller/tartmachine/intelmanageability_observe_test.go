package tartmachine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
)

// TestObserveHostStoppedIntelManageabilityは、Talos shutdown要求後もTalosUnavailableを電源断の代わりに使わず、Intel Manageabilityが報告するPowerStateだけを停止確認の根拠にすることを検証する。
func TestObserveHostStoppedIntelManageability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cimPowerState string
		wantStopped   bool
	}{
		{name: "PowerState On なら停止済みとしない", cimPowerState: "2", wantStopped: false},
		{name: "PowerState Off なら停止確認とする", cimPowerState: "8", wantStopped: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				if !strings.Contains(string(body), "CIM_AssociatedPowerManagementService") {
					http.NotFound(w, r)
					return
				}
				_, _ = io.WriteString(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <wsen:EnumerateResponse xmlns:wsen="http://schemas.xmlsoap.org/ws/2004/09/enumeration">
      <wsman:Items xmlns:wsman="http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd">
        <n1:CIM_AssociatedPowerManagementService xmlns:n1="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_AssociatedPowerManagementService">
          <n1:PowerState>`+tt.cimPowerState+`</n1:PowerState>
        </n1:CIM_AssociatedPowerManagementService>
      </wsman:Items>
    </wsen:EnumerateResponse>
  </s:Body>
</s:Envelope>`)
			}))
			t.Cleanup(server.Close)

			address, err := endpoint.ParseHTTPURL(server.URL)
			if err != nil {
				t.Fatalf("ParseHTTPURL() error = %v", err)
			}

			scheme := runtime.NewScheme()
			if err := infrav1alpha1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme() error = %v", err)
			}
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme() error = %v", err)
			}
			credentialSecret := &corev1.Secret{
				Namespace: "tart-system",
				Name:      "amt-credential",
				Data:      map[string][]byte{"username": []byte("admin"), "password": []byte("secret")},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(credentialSecret).Build()

			host := &infrav1alpha1.TartHost{
				Spec: infrav1alpha1.TartHostSpec{
					Power: infrav1alpha1.PowerSpec{
						Backend: infrav1alpha1.PowerBackendIntelManageability,
						IntelManageability: &infrav1alpha1.IntelManageabilityPowerConfig{
							Address: address,
							CredentialSecretRef: infrav1alpha1.ManagementNamespaceSecretReference{
								Name: "amt-credential",
							},
						},
					},
				},
			}

			reconciler := &TartMachineReconciler{Client: fakeClient, ManagementNamespace: "tart-system"}
			stopped, err := reconciler.observeHostStopped(t.Context(), host, &infrav1alpha1.TartMachine{}, nil)
			if err != nil {
				t.Fatalf("observeHostStopped() error = %v", err)
			}
			if stopped != tt.wantStopped {
				t.Fatalf("observeHostStopped() = %t, want %t", stopped, tt.wantStopped)
			}
		})
	}
}
