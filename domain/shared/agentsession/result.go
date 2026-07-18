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

type Decision interface {
	isDecision()
}

type IssueResult interface {
	Decision
	isIssueResult()
}

type AuthenticationResult interface {
	Decision
	isAuthenticationResult()
}

type IssueAccepted struct {
	Session Session
}

func (IssueAccepted) isDecision()    {}
func (IssueAccepted) isIssueResult() {}

type IssueRejected struct {
	Failure Failure
}

func (IssueRejected) isDecision()    {}
func (IssueRejected) isIssueResult() {}

type AuthenticationAccepted struct {
	Session Session
}

func (AuthenticationAccepted) isDecision()             {}
func (AuthenticationAccepted) isAuthenticationResult() {}

type AuthenticationRejected struct {
	Session Session
	Failure Failure
}

func (AuthenticationRejected) isDecision()             {}
func (AuthenticationRejected) isAuthenticationResult() {}
