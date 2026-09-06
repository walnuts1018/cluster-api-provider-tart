package controlplane

import (
	"bytes"

	"github.com/siderolabs/crypto/x509"
)

// CATrustStageはcontrol-plane MachineのTalos machine configurationから観測したCA rotationの進行段階である。
// Resource StatusのProgram counterとしては扱わず、reconcileのたびにTalosから読み出したissuing/accepted CA構成とactive/pending bundleを比較して再計算する。
type CATrustStage int

const (
	// CATrustStageStableはactive generationのCAだけを信頼し、rotationがまだ開始されていない状態である。
	CATrustStageStable CATrustStage = iota
	// CATrustStageDualTrustはissuing CAがactive(旧)generationのままだが、pending(新)generationのCAもaccepted CAへ追加された状態である。
	CATrustStageDualTrust
	// CATrustStageCutoverはissuing CAがpending(新)generationへ切り替わり、active(旧)generationのCAがまだacceptedとして残っている状態である。
	CATrustStageCutover
	// CATrustStageRotatedはissuing CAがpending(新)generationへ切り替わり、active(旧)generationのCAがacceptedから外れた最終状態である。
	CATrustStageRotated
	// CATrustStageUnknownは観測されたCA構成がactive/pendingいずれのCAとも整合せず、安全に分類できない状態である。この場合は呼び出し側がfail-closedで停止する。
	CATrustStageUnknown
)

// CertBundleはrotation判定に必要なCAの観測値である。Talos machinery型への依存をdomainへ持ち込まず、証明書byte列だけで判定する。
type CertBundle struct {
	Machine              *x509.PEMEncodedCertificateAndKey
	KubernetesAPI        *x509.PEMEncodedCertificateAndKey
	KubernetesAggregator *x509.PEMEncodedCertificateAndKey
}

// ObserveStageは、観測されたissuing/accepted CAがactive/pending bundleのCAとどのように整合するかを判定する。
// 3つのCA全てが同じ段階へ揃っていない場合はUnknownを返す。
func ObserveStage(issuingMachine *x509.PEMEncodedCertificateAndKey, acceptedMachine []*x509.PEMEncodedCertificate, issuingAPI *x509.PEMEncodedCertificateAndKey, acceptedAPI []*x509.PEMEncodedCertificate, issuingAggregator *x509.PEMEncodedCertificateAndKey, acceptedAggregator []*x509.PEMEncodedCertificate, active, pending CertBundle) CATrustStage {
	machineStage := caStage(issuingMachine, acceptedMachine, active.Machine, pending.Machine)
	apiStage := caStage(issuingAPI, acceptedAPI, active.KubernetesAPI, pending.KubernetesAPI)
	aggregatorStage := caStage(issuingAggregator, acceptedAggregator, active.KubernetesAggregator, pending.KubernetesAggregator)

	if machineStage == apiStage && apiStage == aggregatorStage {
		return machineStage
	}
	return CATrustStageUnknown
}

// caStageは単一CAのissuing/accepted観測値から、そのCAだけの進行段階を判定する。
func caStage(issuing *x509.PEMEncodedCertificateAndKey, accepted []*x509.PEMEncodedCertificate, active, pending *x509.PEMEncodedCertificateAndKey) CATrustStage {
	issuingActive := sameCertificate(issuing, active)
	issuingPending := sameCertificate(issuing, pending)
	pendingAccepted := containsAcceptedCertificate(accepted, pending)
	activeAccepted := containsAcceptedCertificate(accepted, active)

	switch {
	case issuingActive && !pendingAccepted:
		return CATrustStageStable
	case issuingActive && pendingAccepted:
		return CATrustStageDualTrust
	case issuingPending && activeAccepted:
		return CATrustStageCutover
	case issuingPending && !activeAccepted:
		return CATrustStageRotated
	default:
		return CATrustStageUnknown
	}
}

func sameCertificate(observed, expected *x509.PEMEncodedCertificateAndKey) bool {
	if observed == nil || expected == nil {
		return false
	}
	return bytes.Equal(observed.Crt, expected.Crt)
}

func containsAcceptedCertificate(accepted []*x509.PEMEncodedCertificate, expected *x509.PEMEncodedCertificateAndKey) bool {
	if expected == nil {
		return false
	}
	for _, ca := range accepted {
		if ca != nil && bytes.Equal(ca.Crt, expected.Crt) {
			return true
		}
	}
	return false
}
