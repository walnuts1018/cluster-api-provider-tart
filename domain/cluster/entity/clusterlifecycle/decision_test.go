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

package clusterlifecycle

import (
	"reflect"
	"testing"
)

func TestDecideはClusterの観測状態をLifecycle命令へ変換する(t *testing.T) {
	tests := []struct {
		name     string
		observed ObservedState
		want     Command
	}{
		{
			name:     "通常状態はActive reconcile",
			observed: ObservedActive{},
			want:     CommandReconcileActive{},
		},
		{
			name:     "削除中はfinalize",
			observed: ObservedDeleting{},
			want:     CommandFinalizeDeleting{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Decide(test.observed)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Decide() = %#v, want %#v", got, test.want)
			}
		})
	}
}
