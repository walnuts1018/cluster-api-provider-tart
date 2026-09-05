package controller

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/boot"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestBuildRedfishBackendUsesProviderManagementSecret(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			writePowerJSON(response, http.StatusOK, map[string]any{"Systems": map[string]any{"@odata.id": "/Systems"}})
		case "/Systems":
			writePowerJSON(response, http.StatusOK, map[string]any{"Members": []map[string]string{{"@odata.id": "/Systems/1"}}})
		case "/Systems/1":
			writePowerJSON(response, http.StatusOK, map[string]any{"PowerState": "Off"})
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core) error = %v", err)
	}
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(infrastructure) error = %v", err)
	}
	credential := &corev1.Secret{
		Namespace: "provider-system",
		Name:      "bmc-credentials",
		Data:      map[string][]byte{"username": []byte("operator"), "password": []byte("secret")},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(credential).Build()
	host := &infrav1alpha1.TartHost{
		Name: "host-a",
		UID:  types.UID("host-a"),
		Spec: infrav1alpha1.TartHostSpec{
			MACAddress: network.MACAddress("00:00:5e:00:53:01"),
			Power: infrav1alpha1.PowerSpec{
				Backend: infrav1alpha1.PowerBackendRedfish,
				Redfish: &infrav1alpha1.RedfishPowerConfig{
					Address: endpoint.HTTPSURL(server.URL),
					CredentialSecretRef: infrav1alpha1.ManagementNamespaceSecretReference{
						Name: credential.Name,
					},
					InsecureSkipVerify: true,
				},
			},
		},
	}
	reconciler := &TartHostReconciler{Client: client, ManagementNamespace: credential.Namespace}
	state, err := func() (boot.PowerState, error) {
		backend, backendErr := reconciler.redfishBackend(t.Context(), host)
		if backendErr != nil {
			return boot.PowerStateUnknown, backendErr
		}
		return backend.PowerState(t.Context())
	}()
	if err != nil {
		t.Fatalf("redfish backend power state error = %v", err)
	}
	if state != boot.PowerStateOff {
		t.Fatalf("power state = %q, want %q", state, boot.PowerStateOff)
	}
}

func writePowerJSON(response http.ResponseWriter, status int, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write(payload); err != nil {
		panic(err)
	}
}
