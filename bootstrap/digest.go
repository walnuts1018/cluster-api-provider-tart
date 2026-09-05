// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

// RedactedConfigurationValueはhash計算前にsecret-bearing configuration valueを置換するmarkerである。
const RedactedConfigurationValue = "<redacted>"

var (
	ErrEffectiveConfigurationIncomplete = errors.New("effective machine configuration is incomplete")
	ErrEffectiveConfigurationInvalid    = errors.New("effective machine configuration is invalid")
)

// DigestEffectiveConfigurationはTalosが解釈したeffective configurationを正規化し、secret-bearing valueをredaction markerへ置換してSHA-256を返す。
// 引数はraw patchではなく、Talos machine configurationのrender後の完全な文書でなければならない。
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
