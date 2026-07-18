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
	"crypto/sha256"
	"crypto/subtle"
)

func DecideIssue(command IssueCommand) IssueResult {
	if !command.Binding.Valid() {
		return IssueRejected{Failure: InvalidBinding{}}
	}
	if command.TTL <= 0 {
		return IssueRejected{Failure: InvalidTTL{}}
	}
	digest := sha256.Sum256([]byte(command.Token))
	return IssueAccepted{
		Session: Session{
			TokenDigest: digest,
			Binding:     command.Binding,
			ExpiresAt:   command.IssuedAt.Add(command.TTL),
		},
	}
}

func DecideAuthentication(command AuthenticateCommand) AuthenticationResult {
	if !command.Session.Binding.Valid() {
		return AuthenticationRejected{Session: command.Session, Failure: InvalidBinding{}}
	}
	if command.Session.Consumed {
		return AuthenticationRejected{Session: command.Session, Failure: TokenAlreadyConsumed{}}
	}
	if !command.ObservedAt.Before(command.Session.ExpiresAt) {
		return AuthenticationRejected{Session: command.Session, Failure: ExpiredToken{}}
	}
	if command.Session.AuthenticationFailures >= MaximumAuthenticationFailures {
		return AuthenticationRejected{Session: command.Session, Failure: TooManyFailures{}}
	}

	providedDigest := sha256.Sum256([]byte(command.Token))
	matchedToken := subtle.ConstantTimeCompare(providedDigest[:], command.Session.TokenDigest[:]) == 1
	matchedBinding := command.Binding == command.Session.Binding
	if matchedToken && matchedBinding {
		session := command.Session
		if command.ConsumeOnPass {
			session.Consumed = true
		}
		return AuthenticationAccepted{Session: session}
	}
	session := command.Session
	session.AuthenticationFailures++
	if session.AuthenticationFailures >= MaximumAuthenticationFailures {
		return AuthenticationRejected{Session: session, Failure: TooManyFailures{}}
	}
	return AuthenticationRejected{Session: session, Failure: BindingMismatch{}}
}
