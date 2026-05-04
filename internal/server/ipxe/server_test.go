package ipxe_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/ipxe"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}
	return s
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
		Status: infrastructurev1alpha1.TartMachineStatus{
			BootstrapToken: token,
		},
	}

	cl := setupFakeClient(t, scheme, host1, machine1, host2, machine2)

	t.Run("ValidRequest_MACAddress", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac="+mac, nil)
		req.Host = "bootstrap.example.invalid"
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

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
		if !strings.Contains(body, "talos.config=http://bootstrap.example.invalid/metadata/default/test-machine-1?token="+token) {
			t.Errorf("body missing talos.config metadata URL: %s", body)
		}
	})

	t.Run("ValidRequest_BootMACAddress", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac="+bootMAC, nil)
		req.Host = "bootstrap.example.invalid"
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "kernel https://example.com/vmlinuz-boot") {
			t.Errorf("body missing kernel image for boot mac: %s", body)
		}
		if !strings.Contains(body, "talos.config=http://bootstrap.example.invalid/metadata/default/test-machine-2?token="+token) {
			t.Errorf("body missing metadata URL for boot mac: %s", body)
		}
	})

	t.Run("MissingMAC", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("HostNotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:aa:bb:cc:dd:ee", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestHandlerServesMetadata(t *testing.T) {
	s := setupScheme(t)
	token := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01"

	t.Run("ValidToken", func(t *testing.T) {
		farFuture := metav1.Now().Add(1 * time.Hour)
		tartMachine := &infrastructurev1alpha1.TartMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-machine",
				Namespace:  "default",
				Generation: 3,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "cluster.x-k8s.io/v1beta1",
						Kind:       "Machine",
						Name:       "capi-machine",
					},
				},
			},
			Status: infrastructurev1alpha1.TartMachineStatus{
				HostRef: &corev1.ObjectReference{
					Name:      "test-host",
					Namespace: "default",
				},
				BootstrapToken:        token,
				ProvisioningStartTime: &metav1.Time{Time: farFuture.Add(-10 * time.Minute)},
				TokenExpiresAt:        &metav1.Time{Time: farFuture},
			},
		}
		capiMachine := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "cluster.x-k8s.io/v1beta1",
				"kind":       "Machine",
				"metadata": map[string]any{
					"name":      "capi-machine",
					"namespace": "default",
				},
				"spec": map[string]any{
					"bootstrap": map[string]any{
						"dataSecretName": "bootstrap-secret",
					},
				},
			},
		}
		bootstrapSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"value": []byte("bootstrap-config"),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine?token="+token, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if body := rec.Body.String(); body != "bootstrap-config" {
			t.Fatalf("body = %q, want %q", body, "bootstrap-config")
		}

		updated := &infrastructurev1alpha1.TartMachine{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine"}, updated); err != nil {
			t.Fatalf("failed to get TartMachine after metadata delivery: %v", err)
		}
		if updated.Status.Ready {
			t.Fatal("ready = true, want false until controller marks the machine ready")
		}
		if updated.Status.BootstrapToken != "" {
			t.Fatalf("bootstrapToken = %q, want empty", updated.Status.BootstrapToken)
		}
		if updated.Status.TokenExpiresAt != nil {
			t.Fatalf("tokenExpiresAt = %#v, want nil", updated.Status.TokenExpiresAt)
		}
		if updated.Status.ProvisioningStartTime == nil {
			t.Fatal("provisioningStartTime = nil, want preserved value")
		}
		if updated.Status.ObservedGeneration != tartMachine.Generation {
			t.Fatalf("observedGeneration = %d, want %d", updated.Status.ObservedGeneration, tartMachine.Generation)
		}
	})

	t.Run("MissingToken", func(t *testing.T) {
		farFuture := metav1.Now().Add(1 * time.Hour)
		tartMachine := &infrastructurev1alpha1.TartMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "cluster.x-k8s.io/v1beta1",
						Kind:       "Machine",
						Name:       "capi-machine",
					},
				},
			},
			Status: infrastructurev1alpha1.TartMachineStatus{
				BootstrapToken: token,
				TokenExpiresAt: &metav1.Time{Time: farFuture},
			},
		}
		capiMachine := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "cluster.x-k8s.io/v1beta1",
				"kind":       "Machine",
				"metadata": map[string]any{
					"name":      "capi-machine",
					"namespace": "default",
				},
				"spec": map[string]any{
					"bootstrap": map[string]any{
						"dataSecretName": "bootstrap-secret",
					},
				},
			},
		}
		bootstrapSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"value": []byte("bootstrap-config"),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("InvalidToken", func(t *testing.T) {
		farFuture := metav1.Now().Add(1 * time.Hour)
		tartMachine := &infrastructurev1alpha1.TartMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "cluster.x-k8s.io/v1beta1",
						Kind:       "Machine",
						Name:       "capi-machine",
					},
				},
			},
			Status: infrastructurev1alpha1.TartMachineStatus{
				BootstrapToken: token,
				TokenExpiresAt: &metav1.Time{Time: farFuture},
			},
		}
		capiMachine := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "cluster.x-k8s.io/v1beta1",
				"kind":       "Machine",
				"metadata": map[string]any{
					"name":      "capi-machine",
					"namespace": "default",
				},
				"spec": map[string]any{
					"bootstrap": map[string]any{
						"dataSecretName": "bootstrap-secret",
					},
				},
			},
		}
		bootstrapSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"value": []byte("bootstrap-config"),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine?token=invalidtoken", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("ExpiredToken", func(t *testing.T) {
		pastTime := metav1.Now().Add(-1 * time.Hour)
		tartMachine := &infrastructurev1alpha1.TartMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "cluster.x-k8s.io/v1beta1",
						Kind:       "Machine",
						Name:       "capi-machine",
					},
				},
			},
			Status: infrastructurev1alpha1.TartMachineStatus{
				BootstrapToken: token,
				TokenExpiresAt: &metav1.Time{Time: pastTime},
			},
		}
		capiMachine := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "cluster.x-k8s.io/v1beta1",
				"kind":       "Machine",
				"metadata": map[string]any{
					"name":      "capi-machine",
					"namespace": "default",
				},
				"spec": map[string]any{
					"bootstrap": map[string]any{
						"dataSecretName": "bootstrap-secret",
					},
				},
			},
		}
		bootstrapSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"value": []byte("bootstrap-config"),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine?token="+token, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("EmptyBootstrapToken", func(t *testing.T) {
		tartMachine := &infrastructurev1alpha1.TartMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "cluster.x-k8s.io/v1beta1",
						Kind:       "Machine",
						Name:       "capi-machine",
					},
				},
			},
		}
		capiMachine := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "cluster.x-k8s.io/v1beta1",
				"kind":       "Machine",
				"metadata": map[string]any{
					"name":      "capi-machine",
					"namespace": "default",
				},
				"spec": map[string]any{
					"bootstrap": map[string]any{
						"dataSecretName": "bootstrap-secret",
					},
				},
			},
		}
		bootstrapSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"value": []byte("bootstrap-config"),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine?token=anything", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusPreconditionFailed)
		}
	})
}

func TestHandlerServesAssets(t *testing.T) {
	s := setupScheme(t)
	cl := setupFakeClient(t, s)

	assetsRoot := t.TempDir()
	assetPath := filepath.Join(assetsRoot, "images", "kernel")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatalf("failed to create asset directory: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("kernel-image"), 0644); err != nil {
		t.Fatalf("failed to write asset: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/images/kernel", nil)
	rec := httptest.NewRecorder()

	ipxe.NewHandler(cl, ipxe.HandlerConfig{AssetsRoot: assetsRoot}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if body := rec.Body.String(); body != "kernel-image" {
		t.Fatalf("body = %q, want %q", body, "kernel-image")
	}
}

func TestHandlerServesHealthEndpoints(t *testing.T) {
	cl := setupFakeClient(t, setupScheme(t))
	for _, path := range []string{"/livez", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestNewServerDisablesLeaderElection(t *testing.T) {
	cl := setupFakeClient(t, setupScheme(t))
	server := ipxe.NewServer(cl, ":8082", "")

	if server.Addr() != ":8082" {
		t.Fatalf("Addr = %q, want %q", server.Addr(), ":8082")
	}
	if server.NeedLeaderElection() {
		t.Fatal("NeedLeaderElection = true, want false")
	}
}


