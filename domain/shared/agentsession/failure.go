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

type Failure interface {
	isFailure()
	String() string
	Error() string
}

type InvalidTTL struct{}

func (InvalidTTL) isFailure() {}

func (InvalidTTL) String() string { return "invalid_ttl" }
func (InvalidTTL) Error() string  { return "invalid_ttl" }

type InvalidBinding struct{}

func (InvalidBinding) isFailure() {}

func (InvalidBinding) String() string { return "invalid_binding" }
func (InvalidBinding) Error() string  { return "invalid_binding" }

type ExpiredToken struct{}

func (ExpiredToken) isFailure() {}

func (ExpiredToken) String() string { return "expired_token" }
func (ExpiredToken) Error() string  { return "expired_token" }

type TokenAlreadyConsumed struct{}

func (TokenAlreadyConsumed) isFailure() {}

func (TokenAlreadyConsumed) String() string { return "token_already_consumed" }
func (TokenAlreadyConsumed) Error() string  { return "token_already_consumed" }

type BindingMismatch struct{}

func (BindingMismatch) isFailure() {}

func (BindingMismatch) String() string { return "binding_mismatch" }
func (BindingMismatch) Error() string  { return "binding_mismatch" }

type TooManyFailures struct{}

func (TooManyFailures) isFailure() {}

func (TooManyFailures) String() string { return "too_many_failures" }
func (TooManyFailures) Error() string  { return "too_many_failures" }
