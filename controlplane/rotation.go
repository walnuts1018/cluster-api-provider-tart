package controlplane

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
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

// ErrCATrustConfigurationIncompleteは観測したmachine configurationにCA rotation判定へ必要なdocumentが欠けていることを示す。
var ErrCATrustConfigurationIncomplete = errors.New("talos machine configuration has no CA rotation documents")

// ObserveCATrustStageはTalosから読み出した稼働中machine configurationのissuing/accepted CAを、active/pending bundleのrotation対象CA(machine、Kubernetes API server、Kubernetes aggregator)と比較して進行段階を判定する。
// 3つのCA全てが同じ段階へ揃っていない場合はUnknownを返し、呼び出し側が個別generationのCA混在を安全側に扱えるようにする。
func ObserveCATrustStage(configuration []byte, active, pending RotationCertificateAuthorities) (CATrustStage, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return CATrustStageUnknown, errors.New("talos machine configuration is empty")
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return CATrustStageUnknown, fmt.Errorf("load talos machine configuration: %w", err)
	}
	if provider.Machine() == nil || provider.Machine().Security() == nil {
		return CATrustStageUnknown, fmt.Errorf("%w: machine security", ErrCATrustConfigurationIncomplete)
	}
	apiConfig := provider.K8sAPIServerCAConfig()
	aggregatorConfig := provider.K8sAggregatorCAConfig()
	if apiConfig == nil || aggregatorConfig == nil {
		return CATrustStageUnknown, fmt.Errorf("%w: Kubernetes API/aggregator CA", ErrCATrustConfigurationIncomplete)
	}
	security := provider.Machine().Security()

	machineStage := caStage(security.IssuingCA(), security.AcceptedCAs(), active.Machine, pending.Machine)
	apiStage := caStage(apiConfig.IssuingCA(), apiConfig.AcceptedCAs(), active.KubernetesAPI, pending.KubernetesAPI)
	aggregatorStage := caStage(aggregatorConfig.IssuingCA(), aggregatorConfig.AcceptedCAs(), active.KubernetesAggregator, pending.KubernetesAggregator)

	if machineStage == apiStage && apiStage == aggregatorStage {
		return machineStage, nil
	}
	return CATrustStageUnknown, nil
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
