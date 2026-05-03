package ipxe_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/ipxe"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func setupFakeClient(t *testing.T, scheme *runtime.Scheme, objects ...client.Object) client.Client {
	t.Helper()
	ro := make([]runtime.Object, 0, len(objects))
	for _, obj := range objects {
		ro = append(ro, obj)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(ro...)
	builder.WithStatusSubresource(&infrastructurev1alpha1.TartHost{}, &infrastructurev1alpha1.TartMachine{})
	builder.WithIndex(&infrastructurev1alpha1.TartHost{}, "spec.macAddress", func(rawObj client.Object) []string {
		host := rawObj.(*infrastructurev1alpha1.TartHost)
		if mac, err := ipxe.NormalizeMAC(host.Spec.MACAddress); err == nil {
			return []string{mac}
		}
		return nil
	})
	builder.WithIndex(&infrastructurev1alpha1.TartHost{}, "spec.bootMACAddress", func(rawObj client.Object) []string {
		host := rawObj.(*infrastructurev1alpha1.TartHost)
		if host.Spec.BootMACAddress != "" {
			if mac, err := ipxe.NormalizeMAC(host.Spec.BootMACAddress); err == nil {
				return []string{mac}
			}
		}
		return nil
	})
	return builder.Build()
}

func TestHandlerDynamicScript(t *testing.T) {
	scheme := setupScheme(t)
	mac := "00:11:22:33:44:55"
	bootMAC := "AA:BB:CC:DD:EE:FF"
	token := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01"

	host1 := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-1",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: mac,
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine-1",
				Namespace: "default",
			},
		},
	}
	machine1 := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-1",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image:        "https://example.com/vmlinuz",
			KernelParams: []string{"console=ttyS0"},
			Initrd:       "https://example.com/initrd",
		},
		Status: infrastructurev1alpha1.TartMachineStatus{
			BootstrapToken: token,
		},
	}

	host2 := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-2",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress:     "11:22:33:44:55:66",
			BootMACAddress: bootMAC,
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine-2",
				Namespace: "default",
			},
		},
	}
	machine2 := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-2",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image: "https://example.com/vmlinuz-boot",
		},
	}

	cl := setupFakeClient(t, scheme, host1, machine1, host2, machine2)

	t.Run("ValidRequest_MACAddress", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac="+mac, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "#!ipxe") {
			t.Errorf("body missing iPXE header: %s", body)
		}
		if !strings.Contains(body, "kernel https://example.com/vmlinuz console=ttyS0") {
			t.Errorf("body missing kernel image and params: %s", body)
		}
		if !strings.Contains(body, "initrd https://example.com/initrd") {
			t.Errorf("body missing initrd: %s", body)
		}
		// Commented out since metadata server is not implemented yet
		// if !strings.Contains(body, "talos.config=http://") {
		// 	t.Errorf("body missing talos.config: %s", body)
		// }
	})

	t.Run("ValidRequest_BootMACAddress", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac="+bootMAC, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "kernel https://example.com/vmlinuz-boot") {
			t.Errorf("body missing kernel image for boot mac: %s", body)
		}
		if strings.Contains(body, "kernel https://example.com/vmlinuz-boot ") {
			t.Errorf("body has trailing space after kernel without params: %s", body)
		}
	})

	t.Run("MissingMAC", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("HostNotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:aa:bb:cc:dd:ee", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestHandlerMetadataSingleUse(t *testing.T) {
	scheme := setupScheme(t)
	mac := "00:11:22:33:44:55"
	token := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01"
	now := metav1.NewTime(time.Now())
	expiresAt := metav1.NewTime(now.Add(10 * time.Minute))

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
		Status: infrastructurev1alpha1.TartMachineStatus{
			BootstrapToken:        token,
			BootstrapSecretName:   "test-bootstrap-data",
			ProvisioningStartTime: &now,
			TokenExpiresAt:        &expiresAt,
		},
	}
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-bootstrap-data",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"value": []byte("machine-config"),
		},
	}

	cl := setupFakeClient(t, scheme, host, machine, bootstrapSecret)
	handler := ipxe.NewHandler(cl)

	req := httptest.NewRequest(http.MethodGet, "/metadata/"+mac+"?token="+token, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "machine-config" {
		t.Fatalf("first body = %q, want %q", got, "machine-config")
	}

	var updated infrastructurev1alpha1.TartMachine
	if err := cl.Get(req.Context(), client.ObjectKey{Name: "test-machine", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get updated machine: %v", err)
	}
	if updated.Status.BootstrapToken != "" {
		t.Fatalf("BootstrapToken = %q, want empty after first download", updated.Status.BootstrapToken)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/metadata/"+mac+"?token="+token, nil)
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusForbidden {
		t.Fatalf("second status = %d, want %d\nbody=%s", secondRec.Code, http.StatusForbidden, secondRec.Body.String())
	}
}

func TestHandlerServesHealthEndpoints(t *testing.T) {
	cl := setupFakeClient(t, setupScheme(t))
	for _, path := range []string{"/livez", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestNewServerDisablesLeaderElection(t *testing.T) {
	cl := setupFakeClient(t, setupScheme(t))
	server := ipxe.NewServer(cl, ":8082")

	if server.Addr() != ":8082" {
		t.Fatalf("Addr = %q, want %q", server.Addr(), ":8082")
	}
	if server.NeedLeaderElection() {
		t.Fatal("NeedLeaderElection = true, want false")
	}
}
