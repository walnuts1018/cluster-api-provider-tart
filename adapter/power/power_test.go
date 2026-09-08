package power

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power/redfish"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power/wol"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestFactorySelectsConfiguredPowerBackend(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		Namespace: "tart-system",
		Name:      "bmc-credential",
		Data:      map[string][]byte{"username": []byte("operator"), "password": []byte("secret")},
	}).Build()

	mac, err := network.ParseMACAddress("00:00:5e:00:53:01")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	broadcast, err := network.ParseUDPAddress("192.0.2.255:9")
	if err != nil {
		t.Fatalf("ParseUDPAddress() error = %v", err)
	}
	address, err := endpoint.ParseHTTPSURL("https://bmc.test.walnuts.dev/redfish/v1")
	if err != nil {
		t.Fatalf("ParseHTTPSURL() error = %v", err)
	}

	tests := []struct {
		name      string
		host      *infrav1alpha1.TartHost
		wantType  string
		wantError string
	}{
		{
			name: "Wake-on-LAN",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				MACAddress: mac,
				Power: infrav1alpha1.PowerSpec{
					Backend:   infrav1alpha1.PowerBackendWakeOnLAN,
					WakeOnLAN: &infrav1alpha1.WakeOnLANPowerConfig{BroadcastAddress: broadcast},
				},
			}},
			wantType: "wol.Backend",
		},
		{
			name: "Redfish",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				Power: infrav1alpha1.PowerSpec{
					Backend: infrav1alpha1.PowerBackendRedfish,
					Redfish: &infrav1alpha1.RedfishPowerConfig{
						Address: address,
						CredentialSecretRef: infrav1alpha1.ManagementNamespaceSecretReference{
							Name: "bmc-credential",
						},
					},
				},
			}},
			wantType: "redfish.Backend",
		},
		{name: "Manual", host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{Power: infrav1alpha1.PowerSpec{Backend: infrav1alpha1.PowerBackendManual}}}, wantError: "manual power backend"},
		{name: "nil host", wantError: "tart host is unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			backend, err := Factory(t.Context(), reader, "tart-system", tt.host)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("Factory() error = %v, want substring %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Factory() error = %v", err)
			}
			switch tt.wantType {
			case "wol.Backend":
				if _, ok := backend.(wol.Backend); !ok {
					t.Fatalf("Factory() type = %T, want wol.Backend", backend)
				}
			case "redfish.Backend":
				if _, ok := backend.(*redfish.Backend); !ok {
					t.Fatalf("Factory() type = %T, want *redfish.Backend", backend)
				}
			}
		})
	}
}

func TestNewRedfishBackendRejectsNilHost(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	if _, err := NewRedfishBackend(t.Context(), reader, "tart-system", nil); err == nil {
		t.Fatal("NewRedfishBackend(nil) error = nil, want validation error")
	}
}

func TestPowerOnHostHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	mac, err := network.ParseMACAddress("00:00:5e:00:53:02")
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	broadcast, err := network.ParseUDPAddress("192.0.2.255:9")
	if err != nil {
		t.Fatalf("ParseUDPAddress() error = %v", err)
	}
	host := &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
		MACAddress: mac,
		Power: infrav1alpha1.PowerSpec{
			Backend:   infrav1alpha1.PowerBackendWakeOnLAN,
			WakeOnLAN: &infrav1alpha1.WakeOnLANPowerConfig{BroadcastAddress: broadcast},
		},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := PowerOnHost(ctx, nil, "", host); !errors.Is(err, context.Canceled) {
		t.Fatalf("PowerOnHost() error = %v, want context.Canceled", err)
	}
}
