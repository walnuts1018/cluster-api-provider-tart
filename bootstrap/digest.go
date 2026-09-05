package bootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
)

// RedactedConfigurationValueはハッシュ計算前に機密を含むconfiguration valueを置換するmarkerである。
const RedactedConfigurationValue = "<redacted>"

var (
	ErrEffectiveConfigurationIncomplete = errors.New("effective machine configuration is incomplete")
	ErrEffectiveConfigurationInvalid    = errors.New("effective machine configuration is invalid")
)

// DigestEffectiveConfigurationはTalosが解釈したeffective configurationを正規化し、機密を含むvalueをredaction markerへ置換してSHA-256を返す。
// 引数はraw patchではなく、Talos machine configurationをrenderした後の完全な文書でなければならない。
func DigestEffectiveConfiguration(completeConfiguration []byte) (string, error) {
	if len(bytes.TrimSpace(completeConfiguration)) == 0 {
		return "", ErrCompleteConfigurationEmpty
	}

	provider, err := configloader.NewFromBytes(completeConfiguration)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrEffectiveConfigurationInvalid, err)
	}
	if !provider.CompleteForBoot() {
		return "", ErrEffectiveConfigurationIncomplete
	}

	redacted := provider.RedactSecrets(RedactedConfigurationValue)
	canonical, err := redacted.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", fmt.Errorf("encode redacted machine configuration: %w", err)
	}
	if len(bytes.TrimSpace(canonical)) == 0 {
		return "", ErrEffectiveConfigurationIncomplete
	}

	digest := sha256.Sum256(canonical)

	return hex.EncodeToString(digest[:]), nil
}
