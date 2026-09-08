package intelmanageability

import (
	"crypto/md5" //nolint:gosec // Intel ME 7.1世代のStandard ManageabilityはHTTP Digest(RFC 2617, MD5)のみを要求し、より強いalgorithmへ選択の余地がない。
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

var errUnsupportedDigestChallenge = errors.New("unsupported digest authentication challenge")

// digestChallengeはWWW-Authenticateヘッダから解析したHTTP Digest challengeである。
type digestChallenge struct {
	realm  string
	nonce  string
	qop    string
	opaque string
}

// parseDigestChallengeはWWW-Authenticateヘッダを解析する。qop=authのみ対応し、auth-intやalgorithm=MD5-sessは要求しない前提で組む。
func parseDigestChallenge(header string) (digestChallenge, error) {
	const digestPrefix = "Digest "
	if !strings.HasPrefix(header, digestPrefix) {
		return digestChallenge{}, fmt.Errorf("%w: %q", errUnsupportedDigestChallenge, header)
	}
	params := parseDigestParams(strings.TrimPrefix(header, digestPrefix))
	challenge := digestChallenge{
		realm:  params["realm"],
		nonce:  params["nonce"],
		qop:    params["qop"],
		opaque: params["opaque"],
	}
	if challenge.realm == "" || challenge.nonce == "" {
		return digestChallenge{}, fmt.Errorf("%w: missing realm or nonce", errUnsupportedDigestChallenge)
	}
	if challenge.qop != "" && !containsToken(challenge.qop, "auth") {
		return digestChallenge{}, fmt.Errorf("%w: unsupported qop %q", errUnsupportedDigestChallenge, challenge.qop)
	}
	return challenge, nil
}

func containsToken(value, token string) bool {
	for candidate := range strings.SplitSeq(value, ",") {
		if strings.TrimSpace(candidate) == token {
			return true
		}
	}
	return false
}

// parseDigestParamsはDigestヘッダのkey="value"形式(comma区切り)を解析する。valueにcommaを含み得るため、単純なstrings.Splitではなく引用符の中を考慮して分割する。
func parseDigestParams(value string) map[string]string {
	params := map[string]string{}
	var key strings.Builder
	var val strings.Builder
	inValue := false
	inQuotes := false
	flush := func() {
		k := strings.TrimSpace(key.String())
		if k != "" {
			params[k] = strings.Trim(strings.TrimSpace(val.String()), `"`)
		}
		key.Reset()
		val.Reset()
		inValue = false
	}
	for _, r := range value {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			if inValue {
				val.WriteRune(r)
			}
		case r == '=' && !inValue && !inQuotes:
			inValue = true
		case r == ',' && !inQuotes:
			flush()
		default:
			if inValue {
				val.WriteRune(r)
			} else {
				key.WriteRune(r)
			}
		}
	}
	flush()
	return params
}

var digestNonceCounter atomic.Uint32

// authorizationHeaderはRFC 2617のDigest認証responseを計算し、Authorizationヘッダの値を返す。
func (c digestChallenge) authorizationHeader(username, password, method, uri string) (string, error) {
	cnonce, err := randomHex(8)
	if err != nil {
		return "", fmt.Errorf("generate digest cnonce: %w", err)
	}
	nc := fmt.Sprintf("%08x", digestNonceCounter.Add(1))

	ha1 := md5Hex(username + ":" + c.realm + ":" + password)
	ha2 := md5Hex(method + ":" + uri)

	var response string
	if c.qop != "" {
		response = md5Hex(strings.Join([]string{ha1, c.nonce, nc, cnonce, "auth", ha2}, ":"))
	} else {
		response = md5Hex(strings.Join([]string{ha1, c.nonce, ha2}, ":"))
	}

	var header strings.Builder
	fmt.Fprintf(&header, `Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`, username, c.realm, c.nonce, uri, response)
	if c.qop != "" {
		fmt.Fprintf(&header, `, qop=auth, nc=%s, cnonce=%q`, nc, cnonce)
	}
	if c.opaque != "" {
		fmt.Fprintf(&header, `, opaque=%q`, c.opaque)
	}
	return header.String(), nil
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value)) //nolint:gosec // RFC 2617 Digest認証の仕様上MD5が固定である。
	return hex.EncodeToString(sum[:])
}

func randomHex(byteLength int) (string, error) {
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
