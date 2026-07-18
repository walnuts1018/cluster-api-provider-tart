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

package agentsession

import (
	"crypto/rand"
	"crypto/sha256"
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

var ErrInvalidDigest = errors.New("session token digest is invalid")

type Token struct {
	value string
}

func (Token) String() string {
	return "[REDACTED]"
}

func (token Token) BearerValue() string {
	return token.value
}

type Binding struct {
	HostUID      string
	OperationUID string
}

func (binding Binding) Valid() bool {
	return binding.HostUID != "" && binding.OperationUID != ""
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
	TokenDigest            Digest
	Binding                Binding
	ExpiresAt              time.Time
	AuthenticationFailures int
	Consumed               bool
}

func Issue(hostUID, operationUID string, now time.Time, ttl time.Duration) (Token, Session, error) {
	binding := Binding{HostUID: hostUID, OperationUID: operationUID}
	if !binding.Valid() {
		return Token{}, Session{}, fmt.Errorf("issue session: %w", InvalidBinding{})
	}
	if ttl <= 0 {
		return Token{}, Session{}, fmt.Errorf("issue session: %w", InvalidTTL{})
	}
	raw := make([]byte, TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, Session{}, fmt.Errorf("generate session token: %w", err)
	}
	token := Token{value: base64.RawURLEncoding.EncodeToString(raw)}
	decision := DecideIssue(IssueCommand{
		Token:    token.value,
		Binding:  binding,
		IssuedAt: now,
		TTL:      ttl,
	})
	switch result := decision.(type) {
	case IssueAccepted:
		return token, result.Session, nil
	case IssueRejected:
		return Token{}, Session{}, fmt.Errorf("issue session: %w", result.Failure)
	default:
		return Token{}, Session{}, fmt.Errorf("issue session: unexpected decision")
	}
}

func Authenticate(
	session Session,
	providedToken, hostUID, operationUID string,
	now time.Time,
) (Session, AuthenticationResult) {
	decision := DecideAuthentication(AuthenticateCommand{
		Session:       session,
		Token:         providedToken,
		Binding:       Binding{HostUID: hostUID, OperationUID: operationUID},
		ObservedAt:    now,
		ConsumeOnPass: false,
	})
	switch result := decision.(type) {
	case AuthenticationAccepted:
		return result.Session, result
	case AuthenticationRejected:
		return result.Session, result
	default:
		return session, AuthenticationRejected{Session: session, Failure: BindingMismatch{}}
	}
}

func Consume(session Session) Session {
	session.Consumed = true
	return session
}
