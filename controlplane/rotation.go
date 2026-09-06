package controlplane

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

// CATrustStageはdomain/controlplaneの同名型へのエイリアスである。
type CATrustStage = domaincontrolplane.CATrustStage

const (
	CATrustStageStable    = domaincontrolplane.CATrustStageStable
	CATrustStageDualTrust = domaincontrolplane.CATrustStageDualTrust
	CATrustStageCutover   = domaincontrolplane.CATrustStageCutover
	CATrustStageRotated   = domaincontrolplane.CATrustStageRotated
	CATrustStageUnknown   = domaincontrolplane.CATrustStageUnknown
)

var ErrCATrustConfigurationIncomplete = errors.New("talos machine configuration has no CA rotation documents")

// ObserveCATrustStageはTalosから読み出した稼働中machine configurationのissuing/accepted CAを、active/pending bundleのrotation対象CAと比較して進行段階を判定する。
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

	return domaincontrolplane.ObserveStage(
		security.IssuingCA(), security.AcceptedCAs(),
		apiConfig.IssuingCA(), apiConfig.AcceptedCAs(),
		aggregatorConfig.IssuingCA(), aggregatorConfig.AcceptedCAs(),
		active, pending,
	), nil
}
