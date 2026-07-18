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

package main

import (
	"testing"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestParseConfigは既定の出力先を使う(t *testing.T) {
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.outputDir != defaultOutputDir {
		t.Fatalf("outputDir = %q, want %q", cfg.outputDir, defaultOutputDir)
	}
}

func TestVerifySimulationはRollback収束を受け入れる(t *testing.T) {
	err := verifySimulation([]attemptEvidence{
		{Name: "wrong-slot-attempt-1", ReportedSlot: "A", Phase: infrastructurev1beta1.TartHostOperationPhaseBootTrial, Attempt: 1},
		{Name: "wrong-slot-attempt-2", ReportedSlot: "A", Phase: infrastructurev1beta1.TartHostOperationPhaseBootTrial, Attempt: 2},
		{Name: "wrong-slot-attempt-3", ReportedSlot: "A", Phase: infrastructurev1beta1.TartHostOperationPhaseRollingBack, Attempt: 3, DegradedReason: "BootFailed"},
		{Name: "target-slot-report-during-rollback", ReportedSlot: "B", Phase: infrastructurev1beta1.TartHostOperationPhaseRollingBack, Attempt: 3, DegradedReason: "BootFailed"},
		{Name: "healthy-rollback-boot", ReportedSlot: "A", Phase: infrastructurev1beta1.TartHostOperationPhaseFailed, Attempt: 3, DegradedReason: "BootFailed"},
	})
	if err != nil {
		t.Fatalf("verifySimulation() error = %v", err)
	}
}
