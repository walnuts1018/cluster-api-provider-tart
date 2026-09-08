package talos

import (
	"context"
	"errors"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	talosmachine "github.com/siderolabs/talos/pkg/machinery/config/machine"
	k8sconfig "github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	configmeta "github.com/siderolabs/talos/pkg/machinery/config/types/meta"
	"github.com/siderolabs/talos/pkg/machinery/role"
)

func TestClientVersionRejectsUnavailableClient(t *testing.T) {
	t.Parallel()

	var client *Client
	_, err := client.Version(t.Context())
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("Version() error = %v, want ErrClientUnavailable", err)
	}
}

func TestClientShutdownRejectsUnavailableClient(t *testing.T) {
	t.Parallel()

	var client *Client
	if err := client.Shutdown(t.Context()); !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("Shutdown() error = %v, want ErrClientUnavailable", err)
	}
}

func TestDialRejectsEmptyEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := DialMaintenance(t.Context(), " \t"); !errors.Is(err, ErrEndpointEmpty) {
		t.Fatalf("DialMaintenance() error = %v, want ErrEndpointEmpty", err)
	}
	if _, err := DialAuthenticated(t.Context(), "", nil, nil, nil); !errors.Is(err, ErrEndpointEmpty) {
		t.Fatalf("DialAuthenticated() error = %v, want ErrEndpointEmpty", err)
	}
}

func TestDialAuthenticatedRejectsInvalidCredentialsBeforeDial(t *testing.T) {
	t.Parallel()

	const endpoint = "192.0.2.1:50000"
	if _, err := DialAuthenticated(t.Context(), endpoint, []byte("not-a-certificate"), []byte("not-a-key"), []byte("not-a-ca")); err == nil {
		t.Fatal("DialAuthenticated() accepted malformed client credentials")
	}

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	certificate, err := bundle.GenerateTalosAPIClientCertificate(role.MakeSet(role.Admin))
	if err != nil {
		t.Fatalf("GenerateTalosAPIClientCertificate() error = %v", err)
	}
	if _, err := DialAuthenticated(t.Context(), endpoint, certificate.Crt, certificate.Key, nil); err == nil {
		t.Fatal("DialAuthenticated() accepted an empty CA bundle")
	}
	if _, err := DialAuthenticatedFromBundle(t.Context(), endpoint, nil); !errors.Is(err, ErrTalosConfigurationInvalid) {
		t.Fatalf("DialAuthenticatedFromBundle() error = %v, want ErrTalosConfigurationInvalid", err)
	}
}

func TestValidateServicesHealthy(t *testing.T) {
	t.Parallel()

	service := func(id string, health *machineapi.ServiceHealth) *machineapi.ServiceInfo {
		return &machineapi.ServiceInfo{Id: id, Health: health}
	}
	tests := map[string]struct {
		response *machineapi.ServiceListResponse
		wantErr  bool
	}{
		"healthy service": {
			response: &machineapi.ServiceListResponse{Messages: []*machineapi.ServiceList{{Services: []*machineapi.ServiceInfo{service("apid", &machineapi.ServiceHealth{Healthy: true})}}}},
		},
		"healthy and unknown service": {
			response: &machineapi.ServiceListResponse{Messages: []*machineapi.ServiceList{{Services: []*machineapi.ServiceInfo{
				service("apid", &machineapi.ServiceHealth{Healthy: true}),
				service("containerd", &machineapi.ServiceHealth{Unknown: true}),
			}}}},
		},
		"unknown service only": {
			response: &machineapi.ServiceListResponse{Messages: []*machineapi.ServiceList{{Services: []*machineapi.ServiceInfo{service("containerd", &machineapi.ServiceHealth{Unknown: true})}}}},
			wantErr:  true,
		},
		"service without health": {
			response: &machineapi.ServiceListResponse{Messages: []*machineapi.ServiceList{{Services: []*machineapi.ServiceInfo{service("containerd", nil)}}}},
			wantErr:  true,
		},
		"unhealthy service": {
			response: &machineapi.ServiceListResponse{Messages: []*machineapi.ServiceList{{Services: []*machineapi.ServiceInfo{service("apid", &machineapi.ServiceHealth{})}}}},
			wantErr:  true,
		},
		"no service state": {
			response: &machineapi.ServiceListResponse{},
			wantErr:  true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validateServicesHealthy(tt.response); (err != nil) != tt.wantErr {
				t.Fatalf("validateServicesHealthy() error = %v, wantErr = %t", err, tt.wantErr)
			}
		})
	}
}

func TestDialAuthenticatedFromWorkerConfigurationDoesNotPanic(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	input, err := generate.NewInput(
		"cluster-a",
		"https://192.0.2.10:6443",
		"1.34.0",
		generate.WithSecretsBundle(bundle),
	)
	if err != nil {
		t.Fatalf("generate.NewInput() error = %v", err)
	}
	provider, err := input.Config(talosmachine.TypeWorker)
	if err != nil {
		t.Fatalf("Config(worker) error = %v", err)
	}
	configuration, err := provider.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		t.Fatalf("EncodeBytes() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("DialAuthenticatedFromConfiguration() panicked for worker configuration: %v", recovered)
		}
	}()
	client, err := DialAuthenticatedFromConfiguration(ctx, "127.0.0.1:50000", configuration)
	if client != nil {
		if closeErr := client.Close(); closeErr != nil {
			t.Fatalf("Client.Close() error = %v", closeErr)
		}
	}
	if err == nil && client == nil {
		t.Fatal("DialAuthenticatedFromConfiguration() returned neither a client nor an error")
	}
}

func TestSetProviderIDWritesAndRejectsConflict(t *testing.T) {
	t.Parallel()

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	input, err := generate.NewInput("cluster-a", "https://192.0.2.10:6443", "1.34.0", generate.WithSecretsBundle(bundle))
	if err != nil {
		t.Fatalf("generate.NewInput() error = %v", err)
	}
	provider, err := input.Config(talosmachine.TypeWorker)
	if err != nil {
		t.Fatalf("Config(worker) error = %v", err)
	}
	configuration, err := provider.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		t.Fatalf("EncodeBytes() error = %v", err)
	}

	const providerID = "tart://host/test"
	patched, err := SetProviderID(configuration, providerID)
	if err != nil {
		t.Fatalf("SetProviderID() error = %v", err)
	}
	patchedProvider, err := configloader.NewFromBytes(patched)
	if err != nil {
		t.Fatalf("configloader.NewFromBytes() error = %v", err)
	}
	values := patchedProvider.K8sKubeletConfig().ExtraArgs()["provider-id"]
	if len(values) != 1 || values[0] != providerID {
		t.Fatalf("provider-id values = %#v, want [%q]", values, providerID)
	}

	if _, err := SetProviderID(patched, "tart://host/other"); !errors.Is(err, ErrProviderIDConflict) {
		t.Fatalf("SetProviderID() conflict error = %v, want ErrProviderIDConflict", err)
	}

	multiple, err := configurationWithProviderIDValues(configuration, []string{providerID, "tart://host/other"})
	if err != nil {
		t.Fatalf("configurationWithProviderIDValues() error = %v", err)
	}
	multipleProvider, err := configloader.NewFromBytes(multiple)
	if err != nil {
		t.Fatalf("configloader.NewFromBytes(multiple) error = %v", err)
	}
	if values := multipleProvider.K8sKubeletConfig().ExtraArgs()["provider-id"]; len(values) != 2 || values[0] != providerID || values[1] != "tart://host/other" {
		t.Fatalf("multiple provider IDs = %#v, want [%q %q]", values, providerID, "tart://host/other")
	}
	_, multipleErr := SetProviderID(multiple, providerID)
	if !errors.Is(multipleErr, ErrProviderIDConflict) {
		t.Fatalf("SetProviderID() multiple-value error = %v, want ErrProviderIDConflict", multipleErr)
	}
}

func configurationWithProviderIDValues(configuration []byte, values []string) ([]byte, error) {
	patch := k8sconfig.NewKubeletConfigV1Alpha1()
	patch.KubeletArgs = configmeta.Args{"provider-id": configmeta.NewArgValue("", values)}
	patchProvider, err := container.New(patch)
	if err != nil {
		return nil, err
	}
	output, err := configpatcher.Apply(configpatcher.WithBytes(configuration), []configpatcher.Patch{configpatcher.NewStrategicMergePatch(patchProvider)})
	if err != nil {
		return nil, err
	}
	return output.Bytes()
}

func TestValidateUpgradeUsesTalosCompatibilityRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		desired string
		valid   bool
	}{
		{name: "supported patch upgrade", current: "v1.14.0", desired: "v1.14.1", valid: true},
		{name: "modern configuration unavailable", current: "v1.13.0", desired: "v1.14.0"},
		{name: "lifecycle API unavailable", current: "v1.12.0", desired: "v1.13.0"},
		{name: "host too old", current: "v1.11.0", desired: "v1.14.0"},
		{name: "downgrade", current: "v1.14.0", desired: "v1.13.0"},
		{name: "invalid desired version", current: "v1.13.0", desired: "not-a-version"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateUpgrade(test.current, test.desired)
			if (err == nil) != test.valid {
				t.Fatalf("ValidateUpgrade(%q, %q) error = %v, valid = %t", test.current, test.desired, err, test.valid)
			}
		})
	}
}

func TestInstallerImageRejectsUnversionedTalosTag(t *testing.T) {
	t.Parallel()

	if _, err := InstallerImage("1.14.0", "schematic"); err == nil {
		t.Fatal("InstallerImage() error = nil, want version prefix validation")
	}
}

func TestInstallerImageRejectsMalformedSchematicID(t *testing.T) {
	t.Parallel()

	for _, schematicID := range []string{"factory/path", "schematic:tag", "schematic@digest", "schematic id"} {
		if _, err := InstallerImage("v1.14.0", schematicID); err == nil {
			t.Fatalf("InstallerImage(%q) error = nil, want schematic ID validation", schematicID)
		}
	}
}
