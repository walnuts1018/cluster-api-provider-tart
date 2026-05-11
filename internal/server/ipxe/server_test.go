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
	testBootstrapHost  = "bootstrap.example.invalid"
	testBootstrapData  = "bootstrap-config"
	testBootstrapToken = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01"
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
					APIVersion: "cluster.x-k8s.io/v1beta2",
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
			"apiVersion": "cluster.x-k8s.io/v1beta2",
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

//nolint:gocyclo // 複数の独立した iPXE 応答シナリオをまとめて検証するため。
func TestHandlerDynamicScript(t *testing.T) {
	scheme := setupScheme(t)
	mac := "00:00:5e:00:53:02"
	bootMAC := "00:00:5e:00:53:11"
	token := testBootstrapToken

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
			Image:        "https://example.com/ubuntu.raw",
			KernelParams: []string{"console=tty0"},
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
				Name:      "test-machine-debian",
				Namespace: "default",
			},
		},
	}
	machineDebian := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-debian",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image: "https://example.com/vmlinuz-preseed",
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
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
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
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
	tokenSecretDebian := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-debian-bootstrap-token",
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

	cl := setupFakeClient(t, scheme, host1, machine1, host2, machine2, host3, machineNoCloud, host4, machineDebian, host5, machineRaw, host6, machineTalos, tokenSecret1, tokenSecret2, tokenSecret3, tokenSecretDebian, tokenSecret5, tokenSecret6)
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
		if !strings.Contains(body, "kernel http://"+testBootstrapHost+"/assets/agent/vmlinuz initrd=agent-initrd tart.config=http://"+testBootstrapHost+"/provisioning/default/test-machine-1/config/"+token+" console=tty0") {
			t.Errorf("body missing agent kernel, config URL, and params: %s", body)
		}
		if !strings.Contains(body, "initrd --name agent-initrd http://"+testBootstrapHost+"/assets/agent/initrd") {
			t.Errorf("body missing agent initrd: %s", body)
		}
		if strings.Contains(body, "ds=nocloud-net") || strings.Contains(body, "talos.config=") || strings.Contains(body, "preseed.cfg") {
			t.Errorf("body unexpectedly contains legacy OS bootstrap params: %s", body)
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
		if !strings.Contains(body, "tart.config=http://"+testBootstrapHost+"/provisioning/default/test-machine-2/config/"+token) {
			t.Errorf("body missing agent config URL for boot mac: %s", body)
		}
		if strings.Contains(body, "kernel https://example.com/vmlinuz-boot") || strings.Contains(body, "initrd=initrd") {
			t.Errorf("body unexpectedly contains legacy direct boot params: %s", body)
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
		if !strings.Contains(body, "tart.config=http://"+testBootstrapHost+"/provisioning/default/test-machine-nocloud/config/"+token) {
			t.Errorf("body missing agent config URL: %s", body)
		}
	})

	t.Run("ValidRequest_DebianAgentConfig", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:22", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "tart.config=http://"+testBootstrapHost+"/provisioning/default/test-machine-debian/config/"+token) {
			t.Errorf("body missing agent config URL: %s", body)
		}
	})

	t.Run("ValidRequest_UbuntuAgentFormat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:23", nil)
		req.Host = testBootstrapHost
		rec := httptest.NewRecorder()

		ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d\nbody=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "tart.config=http://"+testBootstrapHost+"/provisioning/default/test-machine-raw/config/"+token) {
			t.Errorf("body missing agent config URL: %s", body)
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
		if !strings.Contains(body, "tart.config=http://"+testBootstrapHost+"/provisioning/default/test-machine-talos/config/"+token) {
			t.Errorf("body missing agent config URL for Talos: %s", body)
		}
		if strings.Contains(body, "talos.config=") {
			t.Errorf("body unexpectedly contains legacy Talos direct boot param: %s", body)
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
		if body != "#!ipxe\nexit\n" {
			t.Errorf("body = %q, want %q", body, "#!ipxe\nexit\n")
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

func TestHandlerBootsImagingAgentForLinuxCloudImages(t *testing.T) {
	scheme := setupScheme(t)
	token := testBootstrapToken
	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host-linux",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:40",
			Provisioning: infrastructurev1alpha1.TartHostProvisioningSpec{
				Device: "/dev/nvme0n1",
			},
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				Name:      "test-machine-linux",
				Namespace: "default",
			},
		},
	}
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-linux",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartMachineSpec{
			Image:        "/assets/images/ubuntu-24.04.raw",
			KernelParams: []string{"console=ttyS0"},
			Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
				Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
			},
		},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine-linux-bootstrap-token",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
	cl := setupFakeClient(t, scheme, host, machine, tokenSecret)
	svc := setupBootstrapTokenService(t, cl)

	req := httptest.NewRequest(http.MethodGet, "/ipxe?mac=00:00:5e:00:53:40", nil)
	req.Host = testBootstrapHost
	rec := httptest.NewRecorder()

	ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{
		"kernel http://" + testBootstrapHost + "/assets/agent/vmlinuz initrd=agent-initrd",
		"tart.config=http://" + testBootstrapHost + "/provisioning/default/test-machine-linux/config/" + token,
		"console=ttyS0",
		"initrd --name agent-initrd http://" + testBootstrapHost + "/assets/agent/initrd",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "ds=nocloud-net") {
		t.Fatalf("agent boot script must not pass NoCloud directly to the ramdisk kernel:\n%s", body)
	}
}

func TestHandlerServesProvisioningConfig(t *testing.T) {
	scheme := setupScheme(t)
	token := testBootstrapToken
	tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
	tartMachine.Spec = infrastructurev1alpha1.TartMachineSpec{
		Image: "/assets/images/debian-13.raw",
		Bootstrap: infrastructurev1alpha1.TartMachineBootstrapSpec{
			Format: infrastructurev1alpha1.TartMachineBootstrapFormatNoCloud,
		},
	}
	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-host",
			Namespace: "default",
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:41",
			Provisioning: infrastructurev1alpha1.TartHostProvisioningSpec{
				Device: "/dev/nvme0n1",
			},
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				Name:      tartMachine.Name,
				Namespace: tartMachine.Namespace,
			},
		},
	}

	cl := setupFakeClient(t, scheme, tartMachine, capiMachine, bootstrapSecret, tokenSecret, host)
	svc := setupBootstrapTokenService(t, cl)
	req := httptest.NewRequest(http.MethodGet, "/provisioning/default/test-machine/config/"+token, nil)
	req.Host = testBootstrapHost
	rec := httptest.NewRecorder()

	ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"targetDevice":"/dev/nvme0n1"`,
		`"imageUrl":"http://` + testBootstrapHost + `/assets/images/debian-13.raw"`,
		`"repairGPT":true`,
		`"cidataSizeMiB":20`,
		`"completeUrl":"http://` + testBootstrapHost + `/provisioning/default/test-machine/complete/` + token + `"`,
		`"bootstrap":{"format":"NoCloud","userData":"` + testBootstrapData + `"`,
		`"metaData":"instance-id: default-test-machine\nlocal-hostname: test-machine\n"`,
		`"vendorData":"#cloud-config\n{}\n"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}

	remainingSecret := &corev1.Secret{}
	if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret); err != nil {
		t.Fatalf("provisioning config must not consume bootstrap token: %v", err)
	}
}

func TestHandlerCompletesProvisioningAfterAgentFinishes(t *testing.T) {
	scheme := setupScheme(t)
	token := testBootstrapToken
	tartMachine, capiMachine, tokenSecret, bootstrapSecret := metadataObjects(token)
	cl := setupFakeClient(t, scheme, tartMachine, capiMachine, bootstrapSecret, tokenSecret)
	svc := setupBootstrapTokenService(t, cl)

	req := httptest.NewRequest(http.MethodPost, "/provisioning/default/test-machine/complete/"+token, nil)
	rec := httptest.NewRecorder()

	ipxe.NewHandler(cl, ipxe.HandlerConfig{BootstrapTokenSvc: svc, BaseURL: "http://" + testBootstrapHost}).ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusNoContent)
	remainingSecret := &corev1.Secret{}
	if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-token"}, remainingSecret); err == nil {
		t.Fatal("bootstrap token secret still exists after provisioning completion")
	}
	updated := &infrastructurev1alpha1.TartMachine{}
	if err := cl.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "test-machine"}, updated); err != nil {
		t.Fatalf("failed to get TartMachine after provisioning completion: %v", err)
	}
	if updated.Status.ConsumedBootstrapTokenHash != bootstrapTokenHash(token) {
		t.Fatalf("consumedBootstrapTokenHash = %q, want %q", updated.Status.ConsumedBootstrapTokenHash, bootstrapTokenHash(token))
	}
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
