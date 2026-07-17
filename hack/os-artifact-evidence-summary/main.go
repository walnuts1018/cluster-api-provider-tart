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
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultArtifactDir = "dist/os-artifact"
	defaultMarkdown    = "evidence-summary.md"
	defaultJSON        = "evidence-summary.json"

	statusPassed    = "passed"
	statusFailed    = "failed"
	statusMissing   = "missing"
	statusAttention = "attention"
)

type config struct {
	artifactDir   string
	markdownPath  string
	jsonPath      string
	githubSummary string
}

type summary struct {
	GeneratedAtUTC string            `json:"generatedAtUTC"`
	OverallStatus  string            `json:"overallStatus"`
	Scenarios      []scenarioSummary `json:"scenarios"`
}

type scenarioSummary struct {
	Name         string            `json:"name"`
	Title        string            `json:"title"`
	Status       string            `json:"status"`
	Headline     string            `json:"headline"`
	EvidencePath string            `json:"evidencePath"`
	Details      map[string]string `json:"details,omitempty"`
	Issues       []string          `json:"issues,omitempty"`
}

type firstbootEvidence struct {
	Scenario   string            `json:"scenario"`
	BootReport bootReportSummary `json:"bootReport"`
	Root       rootSummary       `json:"root"`
}

type bootReportSummary struct {
	ActiveSlot         string `json:"activeSlot"`
	ArtifactGeneration uint64 `json:"artifactGeneration"`
	StateMounted       bool   `json:"stateMounted"`
	DataMounted        bool   `json:"dataMounted"`
	BootstrapApplied   bool   `json:"bootstrapApplied"`
}

type rootSummary struct {
	Source          string `json:"source"`
	Options         string `json:"options"`
	MountedReadOnly bool   `json:"mountedReadOnly"`
}

type metadataEvidence struct {
	Scenario               string              `json:"scenario"`
	Written                metadataObservation `json:"written"`
	ObservedAfterPowerLoss metadataObservation `json:"observedAfterPowerLoss"`
}

type metadataObservation struct {
	Record metadataRecord `json:"record"`
}

type metadataRecord struct {
	ActiveSlot         string `json:"activeSlot"`
	TargetSlot         string `json:"targetSlot"`
	RollbackSlot       string `json:"rollbackSlot"`
	ArtifactGeneration uint64 `json:"artifactGeneration"`
	RemainingAttempts  int    `json:"remainingAttempts"`
}

type rollbackEvidence struct {
	Attempts []rollbackAttempt `json:"attempts"`
}

type rollbackAttempt struct {
	Attempt        int      `json:"attempt"`
	SelectedEntry  string   `json:"selectedEntry"`
	EntryFilenames []string `json:"entryFilenames"`
}

type simulatorEvidence struct {
	ExpectedSlot string             `json:"expectedSlot"`
	RollbackSlot string             `json:"rollbackSlot"`
	Transitions  map[string]string  `json:"transitions"`
	Attempts     []simulatorAttempt `json:"attempts"`
}

type simulatorAttempt struct {
	Name           string `json:"name"`
	ReportedSlot   string `json:"reportedSlot"`
	Phase          string `json:"phase"`
	Attempt        int32  `json:"attempt"`
	DegradedReason string `json:"degradedReason,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "os artifact evidence summary failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	result := buildSummary(cfg.artifactDir)
	if err := writeSummaryFiles(cfg, result); err != nil {
		return err
	}
	return nil
}

func parseConfig(args []string) (config, error) {
	cfg := config{
		artifactDir:   defaultArtifactDir,
		markdownPath:  defaultMarkdown,
		jsonPath:      defaultJSON,
		githubSummary: os.Getenv("GITHUB_STEP_SUMMARY"),
	}
	flags := flag.NewFlagSet("os-artifact-evidence-summary", flag.ContinueOnError)
	flags.StringVar(&cfg.artifactDir, "artifact-dir", cfg.artifactDir, "Directory that contains the OS Artifact evidence subdirectories.")
	flags.StringVar(&cfg.markdownPath, "markdown-out", cfg.markdownPath, "Output path for the Markdown summary.")
	flags.StringVar(&cfg.jsonPath, "json-out", cfg.jsonPath, "Output path for the machine-readable JSON summary.")
	flags.StringVar(&cfg.githubSummary, "github-step-summary", cfg.githubSummary, "Optional path to the GitHub Actions step summary file.")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(cfg.artifactDir) == "" {
		return config{}, errors.New("--artifact-dir must not be empty")
	}
	if strings.TrimSpace(cfg.markdownPath) == "" {
		return config{}, errors.New("--markdown-out must not be empty")
	}
	if strings.TrimSpace(cfg.jsonPath) == "" {
		return config{}, errors.New("--json-out must not be empty")
	}
	return cfg, nil
}

func buildSummary(artifactDir string) summary {
	scenarios := []scenarioSummary{
		summarizeFirstboot(filepath.Join(artifactDir, "qemu-firstboot", "evidence.json")),
		summarizeBootTrialMetadata(filepath.Join(artifactDir, "boot-trial-metadata-persistence", "evidence.json")),
		summarizeBootloaderRollback(filepath.Join(artifactDir, "bootloader-rollback", "evidence.json")),
		summarizeBootTrialRollback(filepath.Join(artifactDir, "boot-trial-rollback", "evidence.json")),
	}
	overallStatus := statusPassed
	for _, scenario := range scenarios {
		if scenario.Status != statusPassed {
			overallStatus = statusAttention
			break
		}
	}
	return summary{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		OverallStatus:  overallStatus,
		Scenarios:      scenarios,
	}
}

func writeSummaryFiles(cfg config, result summary) error {
	if err := os.MkdirAll(filepath.Dir(cfg.markdownPath), 0o755); err != nil {
		return fmt.Errorf("create markdown output directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.jsonPath), 0o755); err != nil {
		return fmt.Errorf("create json output directory: %w", err)
	}

	markdown := renderMarkdown(result)
	if err := os.WriteFile(cfg.markdownPath, []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write markdown summary: %w", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json summary: %w", err)
	}
	if err := os.WriteFile(cfg.jsonPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write json summary: %w", err)
	}

	if strings.TrimSpace(cfg.githubSummary) != "" {
		if err := os.WriteFile(cfg.githubSummary, []byte(markdown), 0o644); err != nil {
			return fmt.Errorf("write GitHub step summary: %w", err)
		}
	}
	return nil
}

func summarizeFirstboot(path string) scenarioSummary {
	item := scenarioSummary{
		Name:         "firstboot",
		Title:        "First boot",
		EvidencePath: path,
	}
	var evidence firstbootEvidence
	if err := readEvidence(path, &evidence); err != nil {
		return missingScenario(item, err)
	}
	issues := make([]string, 0, 4)
	if !evidence.BootReport.StateMounted {
		issues = append(issues, "state mount が false")
	}
	if !evidence.BootReport.DataMounted {
		issues = append(issues, "data mount が false")
	}
	if !evidence.BootReport.BootstrapApplied {
		issues = append(issues, "bootstrap applied が false")
	}
	if !evidence.Root.MountedReadOnly {
		issues = append(issues, "root mount が read-only ではない")
	}
	item.Details = map[string]string{
		"activeSlot":         evidence.BootReport.ActiveSlot,
		"artifactGeneration": fmt.Sprintf("%d", evidence.BootReport.ArtifactGeneration),
		"rootSource":         evidence.Root.Source,
		"rootOptions":        evidence.Root.Options,
	}
	item.Headline = fmt.Sprintf("slot=%s root=%s bootstrap=%t", evidence.BootReport.ActiveSlot, evidence.Root.Source, evidence.BootReport.BootstrapApplied)
	if len(issues) == 0 {
		item.Status = statusPassed
		return item
	}
	item.Status = statusFailed
	item.Issues = issues
	return item
}

func summarizeBootTrialMetadata(path string) scenarioSummary {
	item := scenarioSummary{
		Name:         "boot-trial-metadata-persistence",
		Title:        "Boot metadata persistence",
		EvidencePath: path,
	}
	var evidence metadataEvidence
	if err := readEvidence(path, &evidence); err != nil {
		return missingScenario(item, err)
	}
	written := evidence.Written.Record
	observed := evidence.ObservedAfterPowerLoss.Record
	issues := make([]string, 0, 2)
	if written != observed {
		issues = append(issues, "power loss 後に boot metadata が一致しない")
	}
	item.Details = map[string]string{
		"activeSlot":         written.ActiveSlot,
		"targetSlot":         written.TargetSlot,
		"rollbackSlot":       written.RollbackSlot,
		"artifactGeneration": fmt.Sprintf("%d", written.ArtifactGeneration),
		"remainingAttempts":  fmt.Sprintf("%d", written.RemainingAttempts),
	}
	item.Headline = fmt.Sprintf("slot %s -> %s rollback=%s attempts=%d", written.ActiveSlot, written.TargetSlot, written.RollbackSlot, written.RemainingAttempts)
	if len(issues) == 0 {
		item.Status = statusPassed
		return item
	}
	item.Status = statusFailed
	item.Issues = issues
	return item
}

func summarizeBootloaderRollback(path string) scenarioSummary {
	item := scenarioSummary{
		Name:         "bootloader-rollback",
		Title:        "Bootloader rollback",
		EvidencePath: path,
	}
	var evidence rollbackEvidence
	if err := readEvidence(path, &evidence); err != nil {
		return missingScenario(item, err)
	}
	issues := make([]string, 0, 4)
	selected := make([]string, 0, len(evidence.Attempts))
	for _, attempt := range evidence.Attempts {
		selected = append(selected, attempt.SelectedEntry)
		want := "target"
		if attempt.Attempt >= 4 {
			want = "rollback"
		}
		if attempt.SelectedEntry != want {
			issues = append(issues, fmt.Sprintf("boot %d selected %q, want %q", attempt.Attempt, attempt.SelectedEntry, want))
		}
	}
	if len(evidence.Attempts) != 4 {
		issues = append(issues, fmt.Sprintf("attempt count = %d, want 4", len(evidence.Attempts)))
	}
	item.Details = map[string]string{
		"attempts": strings.Join(selected, " -> "),
	}
	item.Headline = fmt.Sprintf("entries=%s", strings.Join(selected, " -> "))
	if len(issues) == 0 {
		item.Status = statusPassed
		return item
	}
	item.Status = statusFailed
	item.Issues = issues
	return item
}

func summarizeBootTrialRollback(path string) scenarioSummary {
	item := scenarioSummary{
		Name:         "boot-trial-rollback",
		Title:        "Boot trial rollback simulator",
		EvidencePath: path,
	}
	var evidence simulatorEvidence
	if err := readEvidence(path, &evidence); err != nil {
		return missingScenario(item, err)
	}
	issues := validateSimulatorEvidence(evidence)
	lastPhase := ""
	if len(evidence.Attempts) > 0 {
		lastPhase = evidence.Attempts[len(evidence.Attempts)-1].Phase
	}
	item.Details = map[string]string{
		"expectedSlot": evidence.ExpectedSlot,
		"rollbackSlot": evidence.RollbackSlot,
		"finalPhase":   lastPhase,
	}
	item.Headline = fmt.Sprintf("expected=%s rollback=%s final=%s", evidence.ExpectedSlot, evidence.RollbackSlot, lastPhase)
	if len(issues) == 0 {
		item.Status = statusPassed
		return item
	}
	item.Status = statusFailed
	item.Issues = issues
	return item
}

func validateSimulatorEvidence(evidence simulatorEvidence) []string {
	issues := make([]string, 0, 6)
	if len(evidence.Attempts) != 5 {
		issues = append(issues, fmt.Sprintf("attempt count = %d, want 5", len(evidence.Attempts)))
		return issues
	}
	if evidence.ExpectedSlot != "B" {
		issues = append(issues, fmt.Sprintf("expectedSlot = %q, want %q", evidence.ExpectedSlot, "B"))
	}
	if evidence.RollbackSlot != "A" {
		issues = append(issues, fmt.Sprintf("rollbackSlot = %q, want %q", evidence.RollbackSlot, "A"))
	}
	if got := evidence.Transitions["rollbackStart"]; got != "BootTrial->RollingBack" {
		issues = append(issues, fmt.Sprintf("rollbackStart = %q, want %q", got, "BootTrial->RollingBack"))
	}
	if got := evidence.Transitions["rollbackFinal"]; got != "RollingBack->Failed" {
		issues = append(issues, fmt.Sprintf("rollbackFinal = %q, want %q", got, "RollingBack->Failed"))
	}
	wantPhases := []string{"BootTrial", "BootTrial", "RollingBack", "RollingBack", "Failed"}
	wantAttempts := []int32{1, 2, 3, 3, 3}
	wantSlots := []string{"A", "A", "A", "B", "A"}
	for index, attempt := range evidence.Attempts {
		if attempt.Phase != wantPhases[index] {
			issues = append(issues, fmt.Sprintf("%s phase = %q, want %q", attempt.Name, attempt.Phase, wantPhases[index]))
		}
		if attempt.Attempt != wantAttempts[index] {
			issues = append(issues, fmt.Sprintf("%s attempt = %d, want %d", attempt.Name, attempt.Attempt, wantAttempts[index]))
		}
		if attempt.ReportedSlot != wantSlots[index] {
			issues = append(issues, fmt.Sprintf("%s reportedSlot = %q, want %q", attempt.Name, attempt.ReportedSlot, wantSlots[index]))
		}
	}
	return issues
}

func readEvidence(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return err
	}
	return nil
}

func missingScenario(item scenarioSummary, err error) scenarioSummary {
	item.Status = statusMissing
	item.Headline = "evidence.json が見つからないか読めない"
	item.Issues = []string{err.Error()}
	return item
}

func renderMarkdown(result summary) string {
	var builder strings.Builder
	builder.WriteString("## OS Artifact evidence summary\n\n")
	builder.WriteString(fmt.Sprintf("- Generated at (UTC): `%s`\n", result.GeneratedAtUTC))
	builder.WriteString(fmt.Sprintf("- Overall status: `%s`\n\n", result.OverallStatus))
	builder.WriteString("| Scenario | Status | Headline | Evidence |\n")
	builder.WriteString("| --- | --- | --- | --- |\n")
	for _, item := range result.Scenarios {
		builder.WriteString(fmt.Sprintf(
			"| %s | %s | %s | `%s` |\n",
			item.Title,
			item.Status,
			escapeTable(item.Headline),
			item.EvidencePath,
		))
	}
	builder.WriteString("\n")
	for _, item := range result.Scenarios {
		builder.WriteString(fmt.Sprintf("### %s\n\n", item.Title))
		builder.WriteString(fmt.Sprintf("- Status: `%s`\n", item.Status))
		builder.WriteString(fmt.Sprintf("- Evidence: `%s`\n", item.EvidencePath))
		if len(item.Details) > 0 {
			keys := []string{
				"activeSlot",
				"targetSlot",
				"rollbackSlot",
				"artifactGeneration",
				"remainingAttempts",
				"rootSource",
				"rootOptions",
				"attempts",
				"expectedSlot",
				"finalPhase",
			}
			for _, key := range keys {
				if value, ok := item.Details[key]; ok {
					builder.WriteString(fmt.Sprintf("- %s: `%s`\n", key, value))
				}
			}
		}
		if len(item.Issues) > 0 {
			builder.WriteString("- Issues:\n")
			for _, issue := range item.Issues {
				builder.WriteString(fmt.Sprintf("  - %s\n", issue))
			}
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func escapeTable(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
