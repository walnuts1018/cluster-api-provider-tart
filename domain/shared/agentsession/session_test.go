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

func TestIssueDecideAuthenticationAndConsume(t *testing.T) {
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
	if got := session.TokenDigest.String(); len(got) != 64 || strings.Contains(got, token.BearerValue()) {
		t.Fatalf("stored digest = %q", got)
	}

	accepted, ok := DecideIssue(IssueCommand{
		Token:    token.BearerValue(),
		Binding:  Binding{HostUID: "host-uid", OperationUID: "operation-uid"},
		IssuedAt: now,
		TTL:      DefaultTTL,
	}).(IssueAccepted)
	if !ok {
		t.Fatalf("DecideIssue() = %#v, want IssueAccepted", accepted)
	}

	result := DecideAuthentication(AuthenticateCommand{
		Session:       session,
		Token:         token.BearerValue(),
		Binding:       Binding{HostUID: "host-uid", OperationUID: "operation-uid"},
		ObservedAt:    now,
		ConsumeOnPass: true,
	})
	acceptedAuth, ok := result.(AuthenticationAccepted)
	if !ok {
		t.Fatalf("DecideAuthentication() = %#v, want AuthenticationAccepted", result)
	}
	if !acceptedAuth.Session.Consumed {
		t.Fatal("accepted session is not marked consumed")
	}
}

func TestAuthenticateRejectsInvalidBindingsExpiryConsumptionAndFailureLimit(t *testing.T) {
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
		wantFailure  string
	}{
		{name: "token", session: session, token: "invalid", hostUID: "host-uid", operationUID: "operation-uid", at: now, wantFailure: "binding_mismatch"},
		{name: "host", session: session, token: token.BearerValue(), hostUID: "other-host", operationUID: "operation-uid", at: now, wantFailure: "binding_mismatch"},
		{name: "operation", session: session, token: token.BearerValue(), hostUID: "host-uid", operationUID: "other-operation", at: now, wantFailure: "binding_mismatch"},
		{name: "expiry", session: session, token: token.BearerValue(), hostUID: "host-uid", operationUID: "operation-uid", at: session.ExpiresAt, wantFailure: "expired_token"},
		{name: "consumed", session: Consume(session), token: token.BearerValue(), hostUID: "host-uid", operationUID: "operation-uid", at: now, wantFailure: "token_already_consumed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := DecideAuthentication(AuthenticateCommand{
				Session:       test.session,
				Token:         test.token,
				Binding:       Binding{HostUID: test.hostUID, OperationUID: test.operationUID},
				ObservedAt:    test.at,
				ConsumeOnPass: false,
			})
			rejected, ok := result.(AuthenticationRejected)
			if !ok {
				t.Fatalf("DecideAuthentication() = %#v, want AuthenticationRejected", result)
			}
			if rejected.Failure.String() != test.wantFailure {
				t.Fatalf("failure = %q, want %q", rejected.Failure.String(), test.wantFailure)
			}
		})
	}
}

func TestAuthenticateLocksSessionAfterFiveFailures(t *testing.T) {
	const tooManyFailures = "too_many_failures"

	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	token, session, err := Issue("host-uid", "operation-uid", now, DefaultTTL)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	for range MaximumAuthenticationFailures {
		result := DecideAuthentication(AuthenticateCommand{
			Session:       session,
			Token:         "invalid",
			Binding:       Binding{HostUID: "host-uid", OperationUID: "operation-uid"},
			ObservedAt:    now,
			ConsumeOnPass: false,
		})
		rejected, ok := result.(AuthenticationRejected)
		if !ok {
			t.Fatalf("DecideAuthentication() = %#v, want AuthenticationRejected", result)
		}
		session = rejected.Session
	}
	if session.AuthenticationFailures != MaximumAuthenticationFailures {
		t.Fatalf("AuthenticationFailures = %d, want %d", session.AuthenticationFailures, MaximumAuthenticationFailures)
	}
	if result := DecideAuthentication(AuthenticateCommand{
		Session:       session,
		Token:         token.BearerValue(),
		Binding:       Binding{HostUID: "host-uid", OperationUID: "operation-uid"},
		ObservedAt:    now,
		ConsumeOnPass: false,
	}); result.(AuthenticationRejected).Failure.String() != tooManyFailures {
		t.Fatalf("DecideAuthentication(correct token) = %#v, want %s", result, tooManyFailures)
	}
}
