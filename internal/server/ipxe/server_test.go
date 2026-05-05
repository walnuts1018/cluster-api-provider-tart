package ipxe_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	k8sbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/bootstraptoken"
	applicationbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/application/bootstraptoken"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/ipxe"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testBootstrapHost = "bootstrap.example.invalid"
	testBootstrapData = "bootstrap-config"
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

func setupBootstrapTokenService(t *testing.T, cl client.Client) applicationbootstraptoken.Service {
	t.Helper()
	return k8sbootstraptoken.NewService(cl)
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if rec.Code != expected {
		t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, expected, rec.Body.String())
	}
}

func metadataObjects(token string) (
	*infrastructurev1alpha1.TartMachine,
	*unstructured.Unstructured,
	*corev1.Secret,
	*corev1.Secret,
) {
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
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"value": []byte(testBootstrapData),
		},
	}
	return tartMachine, capiMachine, tokenSecret, bootstrapSecret
}

func bootstrapTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func TestHandlerDynamicScript(t *testing.T) {
	scheme := setupScheme(t)
	mac := "00:00:5e:00:53:02"
	bootMAC := "00:00:5e:00:53:11"
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
			State: infrastructurev1alpha1.TartHostStateProvisioning,
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
			KernelParams: []string{"console=tty0"},
			Initrd:       "https://example.com/initrd",
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
			},
		},
	}

	host2 := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-2",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress:     "00:00:5e:00:53:12",
			BootMACAddress: bootMAC,
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
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
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
			},
		},
	}
	host3 := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-3",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:21",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine-nocloud",
				Namespace: "default",
			},
		},
	}
	machineNoCloud := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-nocloud",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image: "https://example.com/vmlinuz-nocloud",
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
			},
		},
	}
	host4 := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-4",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:22",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine-preseed",
				Namespace: "default",
			},
		},
	}
	machinePreseed := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-preseed",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image: "https://example.com/vmlinuz-preseed",
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatPreseed,
			},
		},
	}
	host5 := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-5",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:23",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine-raw",
				Namespace: "default",
			},
		},
	}
	machineRaw := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-raw",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image: "https://example.com/vmlinuz-raw",
			KernelParams: []string{
				"console=tty0",
			},
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatRaw,
			},
		},
	}
	host6 := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-6",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:24",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine-talos",
				Namespace: "default",
			},
		},
	}
	machineTalos := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-talos",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image: "https://example.com/vmlinuz-talos",
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatTalos,
			},
		},
	}
	tokenSecret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-1-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	tokenSecret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-2-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	tokenSecret3 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-nocloud-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	tokenSecret4 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-preseed-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	tokenSecret5 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-raw-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	tokenSecret6 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-talos-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}

	cl := setupFakeClient(t, scheme, host1, machine1, host2, machine2, host3, machineNoCloud, host4, machinePreseed, host5, machineRaw, host6, machineTalos, tokenSecret1, tokenSecret2, tokenSecret3, tokenSecret4, tokenSecret5, tokenSecret6)
	svc := setupBootstrapTokenService(t, cl)

	t.Run("ValidRequest_MACAddress", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac="+mac, nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "#!ipxe") {
			t.Errorf("body missing iPXE header: %s", body)
		}
		kernelIdx := strings.Index(body, "\nkernel ")
		initrdIdx := strings.Index(body, "\ninitrd ")
		if kernelIdx == -1 || initrdIdx == -1 || kernelIdx > initrdIdx {
			t.Errorf("kernel command must come before initrd command: kernel=%d, initrd=%d\nbody:\n%s", kernelIdx, initrdIdx, body)
		}
		if !strings.Contains(body, "initrd --name initrd https://example.com/initrd") {
			t.Errorf("body missing initrd: %s", body)
		}
		if !strings.Contains(body, "kernel https://example.com/vmlinuz initrd=initrd console=tty0") {
			t.Errorf("body missing kernel image and params: %s", body)
		}
		if !strings.Contains(body, "ds=nocloud-net;s=http://"+testBootstrapHost+"/metadata/default/test-machine-1/nocloud/"+token+"/") {
			t.Errorf("body missing default NoCloud seed URL: %s", body)
		}
	})

	t.Run("ValidRequest_BootMACAddress", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac="+bootMAC, nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "kernel https://example.com/vmlinuz-boot") {
			t.Errorf("body missing kernel image for boot mac: %s", body)
		}
		if strings.Contains(body, "initrd=initrd") {
			t.Errorf("body unexpectedly contains initrd=initrd: %s", body)
		}
		if !strings.Contains(body, "ds=nocloud-net;s=http://"+testBootstrapHost+"/metadata/default/test-machine-2/nocloud/"+token+"/") {
			t.Errorf("body missing default NoCloud seed URL for boot mac: %s", body)
		}
	})

	t.Run("ValidRequest_NoCloudFormat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:21", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "ds=nocloud-net;s=http://"+testBootstrapHost+"/metadata/default/test-machine-nocloud/nocloud/"+token+"/") {
			t.Errorf("body missing NoCloud seed URL: %s", body)
		}
	})

	t.Run("ValidRequest_PreseedFormat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:22", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "auto=true priority=critical url=http://"+testBootstrapHost+"/metadata/default/test-machine-preseed/preseed/"+token+"/preseed.cfg") {
			t.Errorf("body missing preseed URL: %s", body)
		}
	})

	t.Run("ValidRequest_RawFormat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:23", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.Contains(body, "talos.config=") || strings.Contains(body, "ds=nocloud-net") || strings.Contains(body, "preseed.cfg") {
			t.Errorf("raw format unexpectedly added bootstrap params: %s", body)
		}
	})

	t.Run("ValidRequest_TalosFormat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:24", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "talos.config=http://"+testBootstrapHost+"/metadata/default/test-machine-talos/talos/"+token) {
			t.Errorf("body missing explicit talos.config metadata URL: %s", body)
		}
	})

	t.Run("MissingMAC", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("HostNotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:13", nil)
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body := rec.Body.String()
		if body != "#!ipxe\npoweroff\n" {
			t.Errorf("body = %q, want %q", body, "#!ipxe\npoweroff\n")
		}
	})

	t.Run("ProvisionedState_ReturnsExit", func(t *testing.T) {
		provisionedHost := &infrastructurev1alpha1.TartHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-host-provisioned",
				Namespace: "default",
			},
			Spec: infrastructurev1alpha1.TartHostSpec{
				MACAddress: "00:00:5e:00:53:30",
			},
			Status: infrastructurev1alpha1.TartHostStatus{
				State: infrastructurev1alpha1.TartHostStateProvisioned,
				MachineRef: &corev1.ObjectReference{
					Name:      "test-machine-provisioned",
					Namespace: "default",
				},
			},
		}
		provisionedMachine := &infrastructurev1alpha1.TartMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine-provisioned",
				Namespace: "default",
			},
		}
		cl2 := setupFakeClient(t, scheme, provisionedHost, provisionedMachine)
		svc2 := setupBootstrapTokenService(t, cl2)

		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:30", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl2, ipxe.HandlerConfig{BootstrapTokenSvc: svc2, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if body != "#!ipxe\nexit\n" {
			t.Errorf("body = %q, want %q", body, "#!ipxe\nexit\n")
		}
	})

	t.Run("AvailableState_ReturnsPoweroff", func(t *testing.T) {
		availableHost := &infrastructurev1alpha1.TartHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-host-available",
				Namespace: "default",
			},
			Spec: infrastructurev1alpha1.TartHostSpec{
				MACAddress: "00:00:5e:00:53:31",
			},
			Status: infrastructurev1alpha1.TartHostStatus{
				State: infrastructurev1alpha1.TartHostStateAvailable,
			},
		}
		cl3 := setupFakeClient(t, scheme, availableHost)
		svc3 := setupBootstrapTokenService(t, cl3)

		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:31", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl3, ipxe.HandlerConfig{BootstrapTokenSvc: svc3, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if body != "#!ipxe\npoweroff\n" {
			t.Errorf("body = %q, want %q", body, "#!ipxe\npoweroff\n")
		}
	})

	t.Run("ReservedState_ReturnsSleepAndPoweroff", func(t *testing.T) {
		reservedHost := &infrastructurev1alpha1.TartHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-host-reserved",
				Namespace: "default",
			},
			Spec: infrastructurev1alpha1.TartHostSpec{
				MACAddress: "00:00:5e:00:53:32",
			},
			Status: infrastructurev1alpha1.TartHostStatus{
				State: infrastructurev1alpha1.TartHostStateReserved,
				MachineRef: &corev1.ObjectReference{
					Name:      "test-machine-reserved",
					Namespace: "default",
				},
			},
		}
		reservedMachine := &infrastructurev1alpha1.TartMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine-reserved",
				Namespace: "default",
			},
		}
		cl4 := setupFakeClient(t, scheme, reservedHost, reservedMachine)
		svc4 := setupBootstrapTokenService(t, cl4)

		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:32", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()
		ipxe.NewHandler(cl4, ipxe.HandlerConfig{BootstrapTokenSvc: svc4, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if body != "#!ipxe\nsleep 60\npoweroff\n" {
			t.Errorf("body = %q, want %q", body, "#!ipxe\nsleep 60\npoweroff\n")
		}
	})
}

//nolint:gocyclo // multiple independent test scenarios, not business logic
func TestHandlerServesMetadata(t *testing.T) {
	s := setupScheme(t)
	token := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01"

	t.Run("ValidToken", func(t *testing.T) {
		tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/talos/"+token, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		assertStatus(t, rec, http.StatusOK)
		if body := rec.Body.String(); body != testBootstrapData {
			t.Fatalf("body = %q, want %q", body, testBootstrapData)
		}

		updated := &infrastructurev1alpha1.TartMachine{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine"}, updated); err != nil {
			t.Fatalf("failed to get TartMachine after metadata delivery: %v", err)
		}
		if updated.Status.Ready {
			t.Fatal("ready = true, want false until controller marks the machine ready")
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
		remainingSecret := &corev1.Secret{}
		err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret)
		if err == nil {
			t.Fatal("bootstrap token secret still exists after metadata delivery")
		}
	})

	t.Run("NoCloudMetaDataDoesNotConsumeToken", func(t *testing.T) {
		tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/meta-data", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		assertStatus(t, rec, http.StatusOK)
		if body := rec.Body.String(); body != "instance-id: default-test-machine\nlocal-hostname: test-machine\n" {
			t.Fatalf("body = %q, want NoCloud meta-data", body)
		}

		remainingSecret := &corev1.Secret{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret); err != nil {
			t.Fatalf("bootstrap token secret unexpectedly consumed: %v", err)
		}
	})

	t.Run("NoCloudUserDataConsumesToken", func(t *testing.T) {
		tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/user-data", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		assertStatus(t, rec, http.StatusOK)
		if body := rec.Body.String(); body != testBootstrapData {
			t.Fatalf("body = %q, want %q", body, testBootstrapData)
		}

		remainingSecret := &corev1.Secret{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret); err == nil {
			t.Fatal("bootstrap token secret still exists after NoCloud user-data delivery")
		}

		updated := &infrastructurev1alpha1.TartMachine{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine"}, updated); err != nil {
			t.Fatalf("failed to get TartMachine after NoCloud user-data delivery: %v", err)
		}
		if updated.Status.ConsumedBootstrapTokenHash != bootstrapTokenHash(token) {
			t.Fatalf("consumedBootstrapTokenHash = %q, want %q", updated.Status.ConsumedBootstrapTokenHash, bootstrapTokenHash(token))
		}
	})

	t.Run("NoCloudVendorDataDoesNotConsumeToken", func(t *testing.T) {
		tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/vendor-data", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if body := rec.Body.String(); body != "#cloud-config\n{}\n" {
			t.Fatalf("body = %q, want NoCloud vendor-data", body)
		}

		remainingSecret := &corev1.Secret{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret); err != nil {
			t.Fatalf("bootstrap token secret unexpectedly consumed: %v", err)
		}
	})

	t.Run("NoCloudUserDataThenMetaDataStillWorks", func(t *testing.T) {
		tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		userDataReq := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/user-data", nil)
		userDataRec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(userDataRec, userDataReq)

		if userDataRec.Code != http.StatusOK {
			t.Fatalf("user-data status = %d, want %d\nbody=%s", userDataRec.Code, http.StatusOK, userDataRec.Body.String())
		}
		updated := &infrastructurev1alpha1.TartMachine{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine"}, updated); err != nil {
			t.Fatalf("failed to get TartMachine after user-data delivery: %v", err)
		}
		if updated.Status.ConsumedBootstrapTokenHash != bootstrapTokenHash(token) {
			t.Fatalf("consumedBootstrapTokenHash = %q, want %q", updated.Status.ConsumedBootstrapTokenHash, bootstrapTokenHash(token))
		}

		metaDataReq := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/meta-data", nil)
		metaDataRec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(metaDataRec, metaDataReq)

		if metaDataRec.Code != http.StatusOK {
			t.Fatalf("meta-data status = %d, want %d\nbody=%s", metaDataRec.Code, http.StatusOK, metaDataRec.Body.String())
		}
		if body := metaDataRec.Body.String(); body != "instance-id: default-test-machine\nlocal-hostname: test-machine\n" {
			t.Fatalf("meta-data body = %q, want NoCloud meta-data", body)
		}

		vendorDataReq := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/vendor-data", nil)
		vendorDataRec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(vendorDataRec, vendorDataReq)

		if vendorDataRec.Code != http.StatusOK {
			t.Fatalf("vendor-data status = %d, want %d\nbody=%s", vendorDataRec.Code, http.StatusOK, vendorDataRec.Body.String())
		}
		if body := vendorDataRec.Body.String(); body != "#cloud-config\n{}\n" {
			t.Fatalf("vendor-data body = %q, want NoCloud vendor-data", body)
		}
	})

	t.Run("NoCloudUserDataThenMetaDataRejectsDifferentToken", func(t *testing.T) {
		tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		userDataReq := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+token+"/user-data", nil)
		userDataRec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(userDataRec, userDataReq)

		if userDataRec.Code != http.StatusOK {
			t.Fatalf("user-data status = %d, want %d\nbody=%s", userDataRec.Code, http.StatusOK, userDataRec.Body.String())
		}

		otherToken := "ZYXWVUTSRQPONMLKJIHGFEDCBA9876543210abcdefghijklmnopqrstuvwxyz"
		metaDataReq := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/nocloud/"+otherToken+"/meta-data", nil)
		metaDataRec := httptest.NewRecorder()
		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(metaDataRec, metaDataReq)

		if metaDataRec.Code != http.StatusForbidden {
			t.Fatalf("meta-data status = %d, want %d\nbody=%s", metaDataRec.Code, http.StatusForbidden, metaDataRec.Body.String())
		}
	})

	t.Run("PreseedConsumesToken", func(t *testing.T) {
		tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/preseed/"+token+"/preseed.cfg", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if body := rec.Body.String(); body != testBootstrapData {
			t.Fatalf("body = %q, want %q", body, testBootstrapData)
		}

		remainingSecret := &corev1.Secret{}
		if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret); err == nil {
			t.Fatal("bootstrap token secret still exists after preseed delivery")
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
			Spec: infrastructurev1alpha1.TartMachineSpec{
				Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
					Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
				},
			},
			Status: infrastructurev1alpha1.TartMachineStatus{
				TokenExpiresAt: &metav1.Time{Time: farFuture},
			},
		}
		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine-bootstrap-token",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"token": []byte(token),
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
				"value": []byte(testBootstrapData),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

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
			Spec: infrastructurev1alpha1.TartMachineSpec{
				Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
					Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
				},
			},
			Status: infrastructurev1alpha1.TartMachineStatus{
				TokenExpiresAt: &metav1.Time{Time: farFuture},
			},
		}
		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine-bootstrap-token",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"token": []byte(token),
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
				"value": []byte(testBootstrapData),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/talos/invalidtoken", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

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
			Spec: infrastructurev1alpha1.TartMachineSpec{
				Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
					Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
				},
			},
			Status: infrastructurev1alpha1.TartMachineStatus{
				TokenExpiresAt: &metav1.Time{Time: pastTime},
			},
		}
		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-machine-bootstrap-token",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"token": []byte(token),
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
				"value": []byte(testBootstrapData),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/talos/"+token, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

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
				"value": []byte(testBootstrapData),
			},
		}

		cl := setupFakeClient(t, s, tartMachine, capiMachine, bootstrapSecret)
		svc := setupBootstrapTokenService(t, cl)

		req := httptest.NewRequest(http.MethodGet, "/metadata/default/test-machine/talos/anything", nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusPreconditionFailed)
		}
	})
}

func TestHandlerServesAssets(t *testing.T) {
	s := setupScheme(t)
	cl := setupFakeClient(t, s)
	svc := setupBootstrapTokenService(t, cl)

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

	ipxe.NewHandler(cl, ipxe.HandlerConfig{AssetsRoot: assetsRoot, BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if body := rec.Body.String(); body != "kernel-image" {
		t.Fatalf("body = %q, want %q", body, "kernel-image")
	}
}

func TestHandlerServesHealthEndpoints(t *testing.T) {
	cl := setupFakeClient(t, setupScheme(t))
	svc := setupBootstrapTokenService(t, cl)
	for _, path := range []string{"/livez", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestNewServerDisablesLeaderElection(t *testing.T) {
	cl := setupFakeClient(t, setupScheme(t))
	svc := setupBootstrapTokenService(t, cl)
	server := ipxe.NewServer(cl, svc, ":8082", "", "http://192.168.1.100")

	if server.Addr() != ":8082" {
		t.Fatalf("Addr = %q, want %q", server.Addr(), ":8082")
	}
	if server.NeedLeaderElection() {
		t.Fatal("NeedLeaderElection = true, want false")
	}
}
