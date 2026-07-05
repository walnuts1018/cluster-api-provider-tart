package agentsession

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	TokenBytes                    = 32
	DefaultTTL                    = 10 * time.Minute
	MaximumAuthenticationFailures = 5
)

var (
	ErrInvalidTTL     = errors.New("session token TTL must be greater than zero")
	ErrInvalidBinding = errors.New("session token requires host and operation UID")
	ErrInvalidDigest  = errors.New("session token digest is invalid")
	ErrAuthentication = errors.New("session authentication failed")
)

type Token struct {
	value string
}

// Stringはcredentialが構造化logやerror formattingへ偶発的に出ることを防ぐ。
func (Token) String() string {
	return "[REDACTED]"
}

func (token Token) BearerValue() string {
	return token.value
}

type Digest [sha256.Size]byte

func ParseDigest(value string) (Digest, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return Digest{}, ErrInvalidDigest
	}
	var digest Digest
	copy(digest[:], decoded)
	return digest, nil
}

func (digest Digest) String() string {
	return hex.EncodeToString(digest[:])
}

type Session struct {
	Digest                 Digest
	HostUID                string
	OperationUID           string
	ExpiresAt              time.Time
	AuthenticationFailures int
	Consumed               bool
}

type AuthenticationResult string

const (
	AuthenticationAccepted AuthenticationResult = "Accepted"
	AuthenticationRejected AuthenticationResult = "Rejected"
)

func Issue(hostUID, operationUID string, now time.Time, ttl time.Duration) (Token, Session, error) {
	if hostUID == "" || operationUID == "" {
		return Token{}, Session{}, ErrInvalidBinding
	}
	if ttl <= 0 {
		return Token{}, Session{}, ErrInvalidTTL
	}
	raw := make([]byte, TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, Session{}, fmt.Errorf("generate session token: %w", err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(value))
	return Token{value: value}, Session{
		Digest:       digest,
		HostUID:      hostUID,
		OperationUID: operationUID,
		ExpiresAt:    now.Add(ttl),
	}, nil
}

func Authenticate(
	session Session,
	providedToken, hostUID, operationUID string,
	now time.Time,
) (Session, AuthenticationResult) {
	if session.Consumed ||
		!now.Before(session.ExpiresAt) ||
		session.AuthenticationFailures >= MaximumAuthenticationFailures {
		return session, AuthenticationRejected
	}

	providedDigest := sha256.Sum256([]byte(providedToken))
	tokenMatches := subtle.ConstantTimeCompare(providedDigest[:], session.Digest[:]) == 1
	bindingMatches := hostUID == session.HostUID && operationUID == session.OperationUID
	if tokenMatches && bindingMatches {
		return session, AuthenticationAccepted
	}

	failed := session
	failed.AuthenticationFailures++
	return failed, AuthenticationRejected
}

func Consume(session Session) Session {
	consumed := session
	consumed.Consumed = true
	return consumed
}
