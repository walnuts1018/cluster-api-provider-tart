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

package driver

import (
	"errors"
	"testing"
)

func TestDriverErrorClassification(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")
	err := NewError(ErrorTemporary, cause)
	if !IsErrorKind(err, ErrorTemporary) {
		t.Fatal("IsErrorKind() = false, want true")
	}
	if IsErrorKind(err, ErrorAuthenticationFailed) {
		t.Fatal("temporary error was classified as AuthenticationFailed")
	}
	if !errors.Is(err, cause) {
		t.Fatal("driver error does not unwrap its cause")
	}
}
