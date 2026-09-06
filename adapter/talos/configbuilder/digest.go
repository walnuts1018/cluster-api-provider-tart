package configbuilder

import (
	"bytes"
	"fmt"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

// DigestEffectiveConfigurationはTalosが解釈したeffective configurationを正規化し、機密を含むvalueをredaction markerへ置換してSHA-256を返す。
// 引数はraw patchではなく、Talos machine configurationをrenderした後の完全な文書でなければならない。
func DigestEffectiveConfiguration(completeConfiguration []byte) (string, error) {
	if len(bytes.TrimSpace(completeConfiguration)) == 0 {
		return "", domainbootstrap.ErrCompleteConfigurationEmpty
	}

	provider, err := configloader.NewFromBytes(completeConfiguration)
	if err != nil {
		return "", fmt.Errorf("%w: %w", domainbootstrap.ErrEffectiveConfigurationInvalid, err)
	}
	if !provider.CompleteForBoot() {
		return "", domainbootstrap.ErrEffectiveConfigurationIncomplete
	}

	redacted := provider.RedactSecrets(domainbootstrap.RedactedConfigurationValue)
	canonical, err := redacted.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", fmt.Errorf("encode redacted machine configuration: %w", err)
	}
	if len(bytes.TrimSpace(canonical)) == 0 {
		return "", domainbootstrap.ErrEffectiveConfigurationIncomplete
	}

	// 実際のSHA-256計算はdomain層の純粋関数へ委譲し、このpackageはsiderolabs machineryによる
	// 正規化・redactionだけを担当する。
	digest, err := domainbootstrap.ComputeDigest(canonical)
	if err != nil {
		return "", fmt.Errorf("compute machine configuration digest: %w", err)
	}
	return digest, nil
}
