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
	"strings"
	"testing"
	"time"
)

func TestIssueAndAuthenticate(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	token, session, err := Issue("host-uid", "operation-uid", now, DefaultTTL)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if token.BearerValue() == "" || token.String() != "[REDACTED]" {
		t.Fatalf("issued token is not usable or redacted: %q", token)
	}
	if len(token.BearerValue()) < 43 {
		t.Fatalf("token length = %d, want at least 43 base64url characters", len(token.BearerValue()))
	}
	if got := session.Digest.String(); len(got) != 64 || strings.Contains(got, token.BearerValue()) {
		t.Fatalf("stored digest = %q", got)
	}

	updated, result := Authenticate(session, token.BearerValue(), "host-uid", "operation-uid", now)
	if result != AuthenticationAccepted {
		t.Fatalf("Authenticate() result = %q, want %q", result, AuthenticationAccepted)
	}
	if updated.AuthenticationFailures != 0 {
		t.Fatalf("AuthenticationFailures = %d, want 0", updated.AuthenticationFailures)
	}
}

func TestAuthenticateRejectsInvalidBindingsExpiryAndConsumption(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	token, session, err := Issue("host-uid", "operation-uid", now, DefaultTTL)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	tests := []struct {
		name         string
		session      Session
		token        string
		hostUID      string
		operationUID string
		at           time.Time
	}{
		{name: "token", session: session, token: "invalid", hostUID: "host-uid", operationUID: "operation-uid", at: now},
		{name: "host", session: session, token: token.BearerValue(), hostUID: "other-host", operationUID: "operation-uid", at: now},
		{name: "operation", session: session, token: token.BearerValue(), hostUID: "host-uid", operationUID: "other-operation", at: now},
		{name: "expiry", session: session, token: token.BearerValue(), hostUID: "host-uid", operationUID: "operation-uid", at: session.ExpiresAt},
		{name: "consumed", session: Consume(session), token: token.BearerValue(), hostUID: "host-uid", operationUID: "operation-uid", at: now},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, result := Authenticate(test.session, test.token, test.hostUID, test.operationUID, test.at)
			if result != AuthenticationRejected {
				t.Fatalf("Authenticate() result = %q, want %q", result, AuthenticationRejected)
			}
		})
	}
}

func TestAuthenticateLocksSessionAfterFiveFailures(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	token, session, err := Issue("host-uid", "operation-uid", now, DefaultTTL)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	for range MaximumAuthenticationFailures {
		session, _ = Authenticate(session, "invalid", "host-uid", "operation-uid", now)
	}
	if session.AuthenticationFailures != MaximumAuthenticationFailures {
		t.Fatalf("AuthenticationFailures = %d, want %d", session.AuthenticationFailures, MaximumAuthenticationFailures)
	}
	if _, result := Authenticate(session, token.BearerValue(), "host-uid", "operation-uid", now); result != AuthenticationRejected {
		t.Fatalf("Authenticate(correct token) result = %q, want %q", result, AuthenticationRejected)
	}
}
