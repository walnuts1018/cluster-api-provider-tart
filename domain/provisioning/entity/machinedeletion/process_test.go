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

package machinedeletion

import (
	"testing"

	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

func TestDecideは削除観測をfinalizer操作へ閉じ込める(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		observation Observation
		want        Command
	}{
		{
			name:        "HostRefなしはfinalizer解放",
			observation: ObservationHostReferenceAbsent{},
			want:        CommandReleaseFinalizer{},
		},
		{
			name:        "Host確認後はCleaning開始",
			observation: ObservationHostReadyForCleaning{},
			want:        CommandStartCleaning{},
		},
		{
			name:        "Operation消失は参照クリア",
			observation: ObservationCleaningOperationLost{},
			want:        CommandClearOperationReference{},
		},
		{
			name:        "実行中Operationは待機",
			observation: ObservationCleaningOperationRunning{Phase: operationdomain.PhaseWriting},
			want:        CommandWaitCleaning{},
		},
		{
			name:        "成功Operationはfinalizer解放",
			observation: ObservationCleaningOperationSucceeded{},
			want:        CommandReleaseFinalizer{},
		},
		{
			name:        "失敗Operationは削除失敗",
			observation: ObservationCleaningOperationFailed{Phase: operationdomain.PhaseFailed},
			want:        CommandFailCleaning{Phase: operationdomain.PhaseFailed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Decide(tt.observation)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Decide() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
