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

package machinefinalizer

import (
	"reflect"
	"testing"
)

func TestDecideは期待するFinalizer状態への操作だけを返す(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		desired  DesiredState
		observed ObservedState
		want     Command
	}{
		{
			name:     "必要で存在しない場合は追加する",
			desired:  DesiredPresent{},
			observed: ObservedAbsent{},
			want:     CommandAdd{},
		},
		{
			name:     "必要で存在する場合は何もしない",
			desired:  DesiredPresent{},
			observed: ObservedPresent{},
			want:     CommandNoop{},
		},
		{
			name:     "不要で存在する場合は削除する",
			desired:  DesiredAbsent{},
			observed: ObservedPresent{},
			want:     CommandRemove{},
		},
		{
			name:     "不要で存在しない場合は何もしない",
			desired:  DesiredAbsent{},
			observed: ObservedAbsent{},
			want:     CommandNoop{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Decide(tt.desired, tt.observed)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decide() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
