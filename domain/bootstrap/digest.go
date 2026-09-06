package bootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// RedactedConfigurationValueはハッシュ計算前に機密を含むconfiguration valueを置換するmarkerである。
const RedactedConfigurationValue = "<redacted>"

// ErrCanonicalConfigurationEmptyは、digest計算に渡されたcanonical byte列が空であることを示す。
var ErrCanonicalConfigurationEmpty = errors.New("canonical machine configuration is empty")

// ComputeDigestは、redaction・正規化済みのcanonical machine configuration byte列からSHA-256 hex digestを計算する。
// 呼び出し側(adapter/talos/configbuilder)は、machine configurationの解釈・redaction・canonical
// encodingを済ませてからこの関数へ渡す。この関数自体はbyte列に対する計算のみを行い、
// Talos machine configurationの意味を一切解釈しない。
func ComputeDigest(canonical []byte) (string, error) {
	if len(bytes.TrimSpace(canonical)) == 0 {
		return "", ErrCanonicalConfigurationEmpty
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
