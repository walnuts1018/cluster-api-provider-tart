package ipxe_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/ipxe"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = infrastructurev1alpha1.AddToScheme(scheme)
	return scheme
}

func TestHandlerDynamicScript(t *testing.T) {
	scheme := setupScheme()
	mac := "00:11:22:33:44:55"
	token := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01"
	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: mac,
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine",
				Namespace: "default",
			},
		},
	}
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image:        "https://example.com/vmlinuz",
			KernelParams: []string{"console=ttyS0"},
		},
		Status: infrastructurev1alpha1.TartMachineStatus{
			BootstrapToken: token,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(host, machine).Build()

	t.Run("ValidRequest", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac="+mac, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, scheme).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "#!ipxe") {
			t.Errorf("body missing iPXE header: %s", body)
		}
		if !strings.Contains(body, "kernel https://example.com/vmlinuz") {
			t.Errorf("body missing kernel image: %s", body)
		}
		if !strings.Contains(body, "console=ttyS0") {
			t.Errorf("body missing kernel params: %s", body)
		}
		if !strings.Contains(body, "talos.config=http://") {
			t.Errorf("body missing talos.config: %s", body)
		}
		if !strings.Contains(body, token) {
			t.Errorf("body missing token: %s", body)
		}
	})

	t.Run("MissingMAC", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl, scheme).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("HostNotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:aa:bb:cc:dd:ee", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl, scheme).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestHandlerServesHealthEndpoints(t *testing.T) {
	cl := fake.NewClientBuilder().Build()
	scheme := runtime.NewScheme()
	for _, path := range []string{"/livez", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, scheme).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestNewServerDisablesLeaderElection(t *testing.T) {
	cl := fake.NewClientBuilder().Build()
	scheme := runtime.NewScheme()
	server := ipxe.NewServer(cl, scheme, ":8082")

	if server.Addr() != ":8082" {
		t.Fatalf("Addr = %q, want %q", server.Addr(), ":8082")
	}
	if server.NeedLeaderElection() {
		t.Fatal("NeedLeaderElection = true, want false")
	}
}
