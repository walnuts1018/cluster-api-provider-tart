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

	canonicalComplete, err := provider.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", fmt.Errorf("encode complete machine configuration: %w", err)
	}
	if len(bytes.TrimSpace(canonicalComplete)) == 0 {
		return "", domainbootstrap.ErrEffectiveConfigurationIncomplete
	}
	secretFingerprint, err := domainbootstrap.ComputeDigest(canonicalComplete)
	if err != nil {
		return "", fmt.Errorf("compute complete machine configuration fingerprint: %w", err)
	}

	redacted := provider.RedactSecrets(domainbootstrap.RedactedConfigurationValue)
	canonical, err := redacted.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", fmt.Errorf("encode redacted machine configuration: %w", err)
	}
	if len(bytes.TrimSpace(canonical)) == 0 {
		return "", domainbootstrap.ErrEffectiveConfigurationIncomplete
	}

	// 秘密値自体はdigest materialへ入れず、canonicalな完全configurationのfingerprintだけをredacted configurationへ加える。これにより秘密だけの変更もimmutable Secretの世代変更として検出でき、digestから秘密値を復元できない。
	digestMaterial := make([]byte, 0, len(canonical)+len(secretFingerprint)+1)
	digestMaterial = append(digestMaterial, canonical...)
	digestMaterial = append(digestMaterial, '\n')
	digestMaterial = append(digestMaterial, secretFingerprint...)
	digest, err := domainbootstrap.ComputeDigest(digestMaterial)
	if err != nil {
		return "", fmt.Errorf("compute machine configuration digest: %w", err)
	}
	return digest, nil
}
