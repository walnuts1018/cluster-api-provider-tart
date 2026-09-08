package certbuilder

import (
	"bytes"
	"testing"
	"time"

	"github.com/siderolabs/crypto/x509"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	talosmachine "github.com/siderolabs/talos/pkg/machinery/config/machine"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

func TestDecodeBundleDataRestoresClock(t *testing.T) {
	t.Parallel()

	clusterID, err := clusterdomain.ParseClusterID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("ParseClusterID() error = %v", err)
	}
	data, err := GenerateBundleData(clusterID)
	if err != nil {
		t.Fatalf("GenerateBundleData() error = %v", err)
	}
	bundle, err := DecodeBundleData(data, clusterID)
	if err != nil {
		t.Fatalf("DecodeBundleData() error = %v", err)
	}
	if bundle.Clock == nil {
		t.Fatal("DecodeBundleData() returned a bundle with nil Clock")
	}
}

func TestGenerateRotatedBundleDataPreservesServiceAccountKey(t *testing.T) {
	t.Parallel()

	clusterID, err := clusterdomain.ParseClusterID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("ParseClusterID() error = %v", err)
	}
	previous, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	previous.Cluster.ID = clusterID.String()
	serviceAccountKey := bytes.Clone(previous.Certs.K8sServiceAccount.Key)

	data, err := GenerateRotatedBundleData(clusterID, previous)
	if err != nil {
		t.Fatalf("GenerateRotatedBundleData() error = %v", err)
	}
	rotated, err := DecodeBundleData(data, clusterID)
	if err != nil {
		t.Fatalf("DecodeBundleData() rotated error = %v", err)
	}
	if !bytes.Equal(rotated.Certs.K8sServiceAccount.Key, serviceAccountKey) {
		t.Fatal("GenerateRotatedBundleData() regenerated the Kubernetes service-account signing key")
	}
}

func TestObserveAndPatchWorkerCARotationWithoutPrivateKeys(t *testing.T) {
	t.Parallel()

	activeBundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle(active) error = %v", err)
	}
	pendingBundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle(pending) error = %v", err)
	}
	input, err := generate.NewInput("cluster-a", "https://192.0.2.10:6443", "1.34.0", generate.WithSecretsBundle(activeBundle))
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
	active, err := ExtractRotationCertificateAuthorities(activeBundle)
	if err != nil {
		t.Fatalf("ExtractRotationCertificateAuthorities(active) error = %v", err)
	}
	pending, err := ExtractRotationCertificateAuthorities(pendingBundle)
	if err != nil {
		t.Fatalf("ExtractRotationCertificateAuthorities(pending) error = %v", err)
	}

	assertWorkerConfiguration := func(configuration []byte) {
		t.Helper()
		provider, err := configloader.NewFromBytes(configuration)
		if err != nil {
			t.Fatalf("configloader.NewFromBytes() error = %v", err)
		}
		if issuing := provider.Machine().Security().IssuingCA(); issuing == nil || len(issuing.Key) != 0 {
			t.Fatal("worker machine CA unexpectedly contains a private key")
		}
		api := provider.K8sAPIServerCAConfig()
		if api == nil {
			t.Fatal("worker configuration has no Kubernetes API CA")
		}
		if issuing := api.IssuingCA(); issuing != nil && len(issuing.Key) != 0 {
			t.Fatal("worker Kubernetes API CA unexpectedly contains a private key")
		}
		if provider.K8sAggregatorCAConfig() != nil {
			t.Fatal("worker configuration unexpectedly contains a Kubernetes aggregator CA document")
		}
	}

	assertStage := func(name string, configuration []byte, want domaincontrolplane.CATrustStage) {
		t.Helper()
		assertWorkerConfiguration(configuration)
		got, err := ObserveCATrustStage(configuration, active, pending)
		if err != nil {
			t.Fatalf("ObserveCATrustStage(%s) error = %v", name, err)
		}
		if got != want {
			t.Fatalf("ObserveCATrustStage(%s) = %v, want %v", name, got, want)
		}
	}

	assertStage("stable", configuration, domaincontrolplane.CATrustStageStable)
	for _, stage := range []struct {
		name            string
		want            domaincontrolplane.CATrustStage
		machineIssuing  *x509.PEMEncodedCertificateAndKey
		machineAccepted *x509.PEMEncodedCertificateAndKey
		apiIssuing      *x509.PEMEncodedCertificateAndKey
		apiAccepted     *x509.PEMEncodedCertificateAndKey
	}{
		{name: "dual trust", want: domaincontrolplane.CATrustStageDualTrust, machineIssuing: active.Machine, machineAccepted: pending.Machine, apiIssuing: active.KubernetesAPI, apiAccepted: pending.KubernetesAPI},
		{name: "cutover", want: domaincontrolplane.CATrustStageCutover, machineIssuing: pending.Machine, machineAccepted: active.Machine, apiIssuing: pending.KubernetesAPI, apiAccepted: active.KubernetesAPI},
		{name: "rotated", want: domaincontrolplane.CATrustStageRotated, machineIssuing: pending.Machine, apiIssuing: pending.KubernetesAPI},
	} {
		configuration, err = talos.SetMachineCertificateAuthority(configuration, stage.machineIssuing, stage.machineAccepted)
		if err != nil {
			t.Fatalf("SetMachineCertificateAuthority(%s) error = %v", stage.name, err)
		}
		configuration, err = talos.SetKubernetesAPICertificateAuthority(configuration, stage.apiIssuing, stage.apiAccepted)
		if err != nil {
			t.Fatalf("SetKubernetesAPICertificateAuthority(%s) error = %v", stage.name, err)
		}
		configuration, err = talos.SetKubernetesAggregatorCertificateAuthority(configuration, pending.KubernetesAggregator)
		if err != nil {
			t.Fatalf("SetKubernetesAggregatorCertificateAuthority(%s) error = %v", stage.name, err)
		}
		assertStage(stage.name, configuration, stage.want)
	}
}
