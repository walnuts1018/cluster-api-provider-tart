package controlplane

import (
	"testing"

	"github.com/siderolabs/crypto/x509"
)

func TestObserveStage(t *testing.T) {
	t.Parallel()

	active := CertBundle{
		Machine:              rotationTestCertificate("active-machine"),
		KubernetesAPI:        rotationTestCertificate("active-api"),
		KubernetesAggregator: rotationTestCertificate("active-aggregator"),
	}
	pending := CertBundle{
		Machine:              rotationTestCertificate("pending-machine"),
		KubernetesAPI:        rotationTestCertificate("pending-api"),
		KubernetesAggregator: rotationTestCertificate("pending-aggregator"),
	}

	stages := map[string]struct {
		stage              CATrustStage
		issuingMachine     *x509.PEMEncodedCertificateAndKey
		issuingAPI         *x509.PEMEncodedCertificateAndKey
		issuingAggregator  *x509.PEMEncodedCertificateAndKey
		acceptedMachine    []*x509.PEMEncodedCertificate
		acceptedAPI        []*x509.PEMEncodedCertificate
		acceptedAggregator []*x509.PEMEncodedCertificate
	}{
		"stable": {
			stage:              CATrustStageStable,
			issuingMachine:     active.Machine,
			issuingAPI:         active.KubernetesAPI,
			issuingAggregator:  active.KubernetesAggregator,
			acceptedMachine:    rotationTestAccepted(active.Machine),
			acceptedAPI:        rotationTestAccepted(active.KubernetesAPI),
			acceptedAggregator: rotationTestAccepted(active.KubernetesAggregator),
		},
		"dual trust": {
			stage:              CATrustStageDualTrust,
			issuingMachine:     active.Machine,
			issuingAPI:         active.KubernetesAPI,
			issuingAggregator:  active.KubernetesAggregator,
			acceptedMachine:    rotationTestAccepted(active.Machine, pending.Machine),
			acceptedAPI:        rotationTestAccepted(active.KubernetesAPI, pending.KubernetesAPI),
			acceptedAggregator: rotationTestAccepted(active.KubernetesAggregator, pending.KubernetesAggregator),
		},
		"cutover": {
			stage:              CATrustStageCutover,
			issuingMachine:     pending.Machine,
			issuingAPI:         pending.KubernetesAPI,
			issuingAggregator:  pending.KubernetesAggregator,
			acceptedMachine:    rotationTestAccepted(active.Machine),
			acceptedAPI:        rotationTestAccepted(active.KubernetesAPI),
			acceptedAggregator: rotationTestAccepted(active.KubernetesAggregator),
		},
		"rotated": {
			stage:              CATrustStageRotated,
			issuingMachine:     pending.Machine,
			issuingAPI:         pending.KubernetesAPI,
			issuingAggregator:  pending.KubernetesAggregator,
			acceptedMachine:    rotationTestAccepted(pending.Machine),
			acceptedAPI:        rotationTestAccepted(pending.KubernetesAPI),
			acceptedAggregator: rotationTestAccepted(pending.KubernetesAggregator),
		},
	}

	for name, tt := range stages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ObserveStage(tt.issuingMachine, tt.acceptedMachine, tt.issuingAPI, tt.acceptedAPI, tt.issuingAggregator, tt.acceptedAggregator, active, pending); got != tt.stage {
				t.Fatalf("ObserveStage() = %v, want %v", got, tt.stage)
			}
		})
	}

	t.Run("unknown accepted certificate", func(t *testing.T) {
		t.Parallel()
		unknown := rotationTestCertificate("unknown")
		if got := ObserveStage(active.Machine, rotationTestAccepted(active.Machine, unknown), active.KubernetesAPI, rotationTestAccepted(active.KubernetesAPI), active.KubernetesAggregator, rotationTestAccepted(active.KubernetesAggregator), active, pending); got != CATrustStageUnknown {
			t.Fatalf("ObserveStage() = %v, want %v", got, CATrustStageUnknown)
		}
	})
}

func TestObserveStageWithoutAggregator(t *testing.T) {
	t.Parallel()

	active := CertBundle{Machine: rotationTestCertificate("active-machine"), KubernetesAPI: rotationTestCertificate("active-api")}
	pending := CertBundle{Machine: rotationTestCertificate("pending-machine"), KubernetesAPI: rotationTestCertificate("pending-api")}
	tests := map[string]struct {
		issuingMachine  *x509.PEMEncodedCertificateAndKey
		acceptedMachine []*x509.PEMEncodedCertificate
		acceptedAPI     []*x509.PEMEncodedCertificate
		want            CATrustStage
	}{
		"stable":            {issuingMachine: active.Machine, acceptedMachine: rotationTestAccepted(active.Machine), acceptedAPI: rotationTestAccepted(active.KubernetesAPI), want: CATrustStageStable},
		"dual trust":        {issuingMachine: active.Machine, acceptedMachine: rotationTestAccepted(active.Machine, pending.Machine), acceptedAPI: rotationTestAccepted(active.KubernetesAPI, pending.KubernetesAPI), want: CATrustStageDualTrust},
		"cutover":           {issuingMachine: pending.Machine, acceptedMachine: rotationTestAccepted(active.Machine), acceptedAPI: rotationTestAccepted(active.KubernetesAPI, pending.KubernetesAPI), want: CATrustStageCutover},
		"rotated":           {issuingMachine: pending.Machine, acceptedMachine: rotationTestAccepted(pending.Machine), acceptedAPI: rotationTestAccepted(pending.KubernetesAPI), want: CATrustStageRotated},
		"mismatched stages": {issuingMachine: active.Machine, acceptedMachine: rotationTestAccepted(active.Machine), acceptedAPI: rotationTestAccepted(active.KubernetesAPI, pending.KubernetesAPI), want: CATrustStageUnknown},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ObserveStageWithoutAggregator(tt.issuingMachine, tt.acceptedMachine, nil, tt.acceptedAPI, active, pending); got != tt.want {
				t.Fatalf("ObserveStageWithoutAggregator() = %v, want %v", got, tt.want)
			}
		})
	}
}

func rotationTestCertificate(value string) *x509.PEMEncodedCertificateAndKey {
	return &x509.PEMEncodedCertificateAndKey{Crt: []byte(value), Key: []byte(value + "-key")}
}

func rotationTestAccepted(certificates ...*x509.PEMEncodedCertificateAndKey) []*x509.PEMEncodedCertificate {
	accepted := make([]*x509.PEMEncodedCertificate, 0, len(certificates))
	for _, certificate := range certificates {
		accepted = append(accepted, &x509.PEMEncodedCertificate{Crt: certificate.Crt})
	}
	return accepted
}
