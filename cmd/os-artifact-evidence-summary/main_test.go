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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigは既定値を解決する(t *testing.T) {
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.artifactDir != defaultArtifactDir {
		t.Fatalf("artifactDir = %q, want %q", cfg.artifactDir, defaultArtifactDir)
	}
	if cfg.markdownPath != defaultMarkdown {
		t.Fatalf("markdownPath = %q, want %q", cfg.markdownPath, defaultMarkdown)
	}
	if cfg.jsonPath != defaultJSON {
		t.Fatalf("jsonPath = %q, want %q", cfg.jsonPath, defaultJSON)
	}
}

func TestBuildSummaryは主要シナリオを要約する(t *testing.T) {
	artifactDir := t.TempDir()
	writeEvidenceFixture(t, filepath.Join(artifactDir, "qemu-firstboot", "evidence.json"), map[string]any{
		"scenario": "firstboot",
		"bootReport": map[string]any{
			"activeSlot":         "A",
			"artifactGeneration": 1,
			"stateMounted":       true,
			"dataMounted":        true,
			"bootstrapApplied":   true,
		},
		"root": map[string]any{
			"source":          "/dev/vda",
			"options":         "ro,relatime",
			"mountedReadOnly": true,
		},
	})
	writeEvidenceFixture(t, filepath.Join(artifactDir, "boot-trial-metadata-persistence", "evidence.json"), map[string]any{
		"scenario": "boot-trial-metadata-persistence",
		"written": map[string]any{
			"record": map[string]any{
				"activeSlot":         "A",
				"targetSlot":         "B",
				"rollbackSlot":       "A",
				"artifactGeneration": 2,
				"remainingAttempts":  2,
			},
		},
		"observedAfterPowerLoss": map[string]any{
			"record": map[string]any{
				"activeSlot":         "A",
				"targetSlot":         "B",
				"rollbackSlot":       "A",
				"artifactGeneration": 2,
				"remainingAttempts":  2,
			},
		},
	})
	writeEvidenceFixture(t, filepath.Join(artifactDir, "bootloader-rollback", "evidence.json"), map[string]any{
		"attempts": []map[string]any{
			{"attempt": 1, "selectedEntry": "target"},
			{"attempt": 2, "selectedEntry": "target"},
			{"attempt": 3, "selectedEntry": "target"},
			{"attempt": 4, "selectedEntry": "rollback"},
		},
	})
	writeEvidenceFixture(t, filepath.Join(artifactDir, "boot-trial-rollback", "evidence.json"), map[string]any{
		"expectedSlot": "B",
		"rollbackSlot": "A",
		"transitions": map[string]any{
			"rollbackStart": "BootTrial->RollingBack",
			"rollbackFinal": "RollingBack->Failed",
		},
		"attempts": []map[string]any{
			{"name": "wrong-slot-attempt-1", "reportedSlot": "A", "phase": "BootTrial", "attempt": 1},
			{"name": "wrong-slot-attempt-2", "reportedSlot": "A", "phase": "BootTrial", "attempt": 2},
			{"name": "wrong-slot-attempt-3", "reportedSlot": "A", "phase": "RollingBack", "attempt": 3},
			{"name": "target-slot-report-during-rollback", "reportedSlot": "B", "phase": "RollingBack", "attempt": 3},
			{"name": "healthy-rollback-boot", "reportedSlot": "A", "phase": "Failed", "attempt": 3},
		},
	})

	got := buildSummary(artifactDir)
	if got.OverallStatus != statusPassed {
		t.Fatalf("OverallStatus = %q, want %s", got.OverallStatus, statusPassed)
	}
	if len(got.Scenarios) != 4 {
		t.Fatalf("scenario count = %d, want 4", len(got.Scenarios))
	}
	for _, scenario := range got.Scenarios {
		if scenario.Status != statusPassed {
			t.Fatalf("%s status = %q, want %s", scenario.Name, scenario.Status, statusPassed)
		}
	}
	markdown := renderMarkdown(got)
	for _, want := range []string{
		"## OS Artifact evidence summary",
		"First boot",
		"Boot metadata persistence",
		"Bootloader rollback",
		"Boot trial rollback simulator",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("renderMarkdown() does not contain %q\n%s", want, markdown)
		}
	}
}

func TestBuildSummaryは欠落した証跡をmissing扱いにする(t *testing.T) {
	artifactDir := t.TempDir()

	got := buildSummary(artifactDir)
	if got.OverallStatus != statusAttention {
		t.Fatalf("OverallStatus = %q, want %s", got.OverallStatus, statusAttention)
	}
	for _, scenario := range got.Scenarios {
		if scenario.Status != statusMissing {
			t.Fatalf("%s status = %q, want %s", scenario.Name, scenario.Status, statusMissing)
		}
		if len(scenario.Issues) == 0 {
			t.Fatalf("%s issues are empty", scenario.Name)
		}
	}
}

func TestWriteSummaryFilesはMarkdownとJSONを出力する(t *testing.T) {
	workDir := t.TempDir()
	githubSummary := filepath.Join(workDir, "step-summary.md")
	cfg := config{
		markdownPath:  filepath.Join(workDir, "summary.md"),
		jsonPath:      filepath.Join(workDir, "summary.json"),
		githubSummary: githubSummary,
	}
	input := summary{
		GeneratedAtUTC: "2026-07-17T00:00:00Z",
		OverallStatus:  statusPassed,
		Scenarios: []scenarioSummary{
			{
				Name:         "firstboot",
				Title:        "First boot",
				Status:       statusPassed,
				Headline:     "slot=A",
				EvidencePath: "dist/os-artifact/qemu-firstboot/evidence.json",
			},
		},
	}

	if err := writeSummaryFiles(cfg, input); err != nil {
		t.Fatalf("writeSummaryFiles() error = %v", err)
	}

	markdownData, err := os.ReadFile(cfg.markdownPath)
	if err != nil {
		t.Fatalf("os.ReadFile(markdown) error = %v", err)
	}
	if !strings.Contains(string(markdownData), "First boot") {
		t.Fatalf("markdown output = %s", markdownData)
	}

	jsonData, err := os.ReadFile(cfg.jsonPath)
	if err != nil {
		t.Fatalf("os.ReadFile(json) error = %v", err)
	}
	var decoded summary
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.OverallStatus != statusPassed {
		t.Fatalf("decoded OverallStatus = %q, want %s", decoded.OverallStatus, statusPassed)
	}

	stepSummaryData, err := os.ReadFile(githubSummary)
	if err != nil {
		t.Fatalf("os.ReadFile(step summary) error = %v", err)
	}
	if string(stepSummaryData) != string(markdownData) {
		t.Fatalf("step summary differs from markdown output")
	}
}

func writeEvidenceFixture(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}
