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

package result_test

import (
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
)

func TestResultSkipsNextStepAfterFailure(t *testing.T) {
	t.Parallel()

	initial := result.Failure[int]("invalid host")
	called := false
	actual := result.Bind(initial, func(value int) result.Result[string, string] {
		called = true
		return result.Success[string, string]("unexpected")
	})
	if called {
		t.Fatal("失敗trackで次の処理が実行された")
	}
	failure, present := actual.FailureValue().Value()
	if !present || failure != "invalid host" {
		t.Fatalf("FailureValue() = %q, %t, want %q, true", failure, present, "invalid host")
	}
}
