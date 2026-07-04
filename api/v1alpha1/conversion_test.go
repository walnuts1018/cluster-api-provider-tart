package v1alpha1

import (
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestTartMachineV1Alpha1FixtureConvertsToV1Beta1(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/tartmachine-v1alpha1.yaml")
	if err != nil {
		t.Fatalf("fixtureを読み込めません: %v", err)
	}
	var source TartMachine
	if err := yaml.Unmarshal(data, &source); err != nil {
		t.Fatalf("fixtureをデコードできません: %v", err)
	}

	var converted infrastructurev1beta1.TartMachine
	if err := source.ConvertTo(&converted); err != nil {
		t.Fatalf("ConvertTo() error = %v", err)
	}

	if converted.Spec.ProviderID != source.Spec.ProviderID {
		t.Fatalf("providerID = %q, want %q", converted.Spec.ProviderID, source.Spec.ProviderID)
	}
	if !strings.HasPrefix(converted.Spec.Image.Ref, "oci://migration.invalid/legacy@sha256:") {
		t.Fatalf("image.ref = %q, want migration placeholder", converted.Spec.Image.Ref)
	}
	if converted.Spec.PlatformProfile != legacyPlatformProfile {
		t.Fatalf("platformProfile = %q, want %q", converted.Spec.PlatformProfile, legacyPlatformProfile)
	}
	if converted.Status.HostRef == nil || converted.Status.HostRef.UID != source.Status.HostRef.UID {
		t.Fatalf("hostRef = %#v, want UID %q", converted.Status.HostRef, source.Status.HostRef.UID)
	}
	if converted.Status.Initialization.Provisioned == nil || !*converted.Status.Initialization.Provisioned {
		t.Fatalf("initialization.provisioned = %#v, want true", converted.Status.Initialization.Provisioned)
	}
}

func TestTartHostConversionMovesMachineReferenceToConsumerReference(t *testing.T) {
	t.Parallel()

	source := TartHost{}
	source.Spec.MACAddress = "02:00:00:00:00:01"
	source.Status.State = TartHostStateReserved
	source.Status.MachineRef = &corev1.ObjectReference{
		Namespace: "default",
		Name:      "machine-a",
		UID:       types.UID("machine-a-uid"),
	}

	var converted infrastructurev1beta1.TartHost
	if err := source.ConvertTo(&converted); err != nil {
		t.Fatalf("ConvertTo() error = %v", err)
	}

	if converted.Spec.Identifiers.BootMACAddress != source.Spec.MACAddress {
		t.Fatalf("bootMACAddress = %q, want %q", converted.Spec.Identifiers.BootMACAddress, source.Spec.MACAddress)
	}
	if converted.Spec.ConsumerRef == nil || converted.Spec.ConsumerRef.UID != source.Status.MachineRef.UID {
		t.Fatalf("consumerRef = %#v, want UID %q", converted.Spec.ConsumerRef, source.Status.MachineRef.UID)
	}
}

func TestV1Beta1FieldsSurviveDownAndUpConversion(t *testing.T) {
	t.Parallel()

	source := &infrastructurev1beta1.TartCluster{
		Spec: infrastructurev1beta1.TartClusterSpec{
			ArtifactPolicy: infrastructurev1beta1.ArtifactPolicy{
				AllowedRegistries: []string{"registry.sample.walnuts.dev"},
			},
		},
	}
	var spoke TartCluster
	if err := spoke.ConvertFrom(source); err != nil {
		t.Fatalf("ConvertFrom() error = %v", err)
	}
	var restored infrastructurev1beta1.TartCluster
	if err := spoke.ConvertTo(&restored); err != nil {
		t.Fatalf("ConvertTo() error = %v", err)
	}

	if len(restored.Spec.ArtifactPolicy.AllowedRegistries) != 1 ||
		restored.Spec.ArtifactPolicy.AllowedRegistries[0] != "registry.sample.walnuts.dev" {
		t.Fatalf("allowedRegistries = %#v, want preserved registry", restored.Spec.ArtifactPolicy.AllowedRegistries)
	}
}
