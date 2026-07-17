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
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	defaultMatrixPath       = "docs/release/release-matrix.yaml"
	defaultReleaseNotePath  = "docs/release-notes/unreleased.md"
	statusSupported         = "Supported"
	statusExperimental      = "Experimental"
	experimentalSectionName = "Experimental"
)

var headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

type options struct {
	matrixPath      string
	releaseNotePath string
}

type matrixEntry struct {
	Name          string
	Category      string
	Status        string
	EvidencePaths []string
	Notes         []string
}

type releaseNote struct {
	sections map[string][]markdownBullet
}

type rawMatrixEntry struct {
	Name          string   `json:"name"`
	Category      string   `json:"category"`
	Status        string   `json:"status"`
	EvidencePaths []string `json:"evidencePaths"`
	Notes         any      `json:"notes"`
}

type knownConstraintRule struct {
	name          string
	keywordGroups [][]string
}

type markdownBullet struct {
	indent int
	text   string
}

var requiredKnownConstraints = []knownConstraintRule{
	{
		name: "single control plane KubernetesBinary update remains Experimental",
		keywordGroups: [][]string{
			{"single control plane"},
			{"kubernetesbinary"},
			{"experimental"},
			{"feature gate"},
		},
	},
	{
		name: "management API outage blocks promotion to Supported",
		keywordGroups: [][]string{
			{"management api outage"},
			{"supported"},
		},
	},
	{
		name: "StateMigration recovery is manual",
		keywordGroups: [][]string{
			{"statemigration"},
			{"recoveryrequired"},
			{"manual recovery", "手動復旧"},
		},
	},
}

func main() {
	opts := options{}
	flag.StringVar(&opts.matrixPath, "matrix", defaultMatrixPath, "Path to docs/release/release-matrix.yaml")
	flag.StringVar(&opts.releaseNotePath, "release-note", defaultReleaseNotePath, "Path to docs/release-notes/unreleased.md")
	flag.Parse()

	if err := run(opts); err != nil {
		slog.Error("release docs validation failed", "error", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	entries, err := loadMatrix(opts.matrixPath)
	if err != nil {
		return err
	}
	notes, err := loadReleaseNote(opts.releaseNotePath)
	if err != nil {
		return err
	}
	return validate(entries, notes)
}

func loadMatrix(path string) ([]matrixEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read matrix %s: %w", path, err)
	}

	entries, err := decodeMatrixEntries(data)
	if err != nil {
		return nil, fmt.Errorf("decode matrix %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, errors.New("matrix must contain at least one entry")
	}
	return entries, nil
}

func decodeMatrixEntries(data []byte) ([]matrixEntry, error) {
	var direct []rawMatrixEntry
	if err := yaml.UnmarshalStrict(data, &direct); err == nil {
		return normalizeMatrixEntries(direct)
	}

	candidates := []struct {
		dest any
	}{
		{dest: &struct {
			ReleaseCandidate any              `json:"releaseCandidate"`
			Entries          []rawMatrixEntry `json:"entries"`
		}{}},
		{dest: &struct {
			ReleaseCandidate any              `json:"releaseCandidate"`
			Rows             []rawMatrixEntry `json:"rows"`
		}{}},
		{dest: &struct {
			ReleaseCandidate any              `json:"releaseCandidate"`
			Matrix           []rawMatrixEntry `json:"matrix"`
		}{}},
	}
	for _, candidate := range candidates {
		if err := yaml.UnmarshalStrict(data, candidate.dest); err != nil {
			continue
		}
		switch value := candidate.dest.(type) {
		case *struct {
			ReleaseCandidate any              `json:"releaseCandidate"`
			Entries          []rawMatrixEntry `json:"entries"`
		}:
			if len(value.Entries) > 0 {
				return normalizeMatrixEntries(value.Entries)
			}
		case *struct {
			ReleaseCandidate any              `json:"releaseCandidate"`
			Rows             []rawMatrixEntry `json:"rows"`
		}:
			if len(value.Rows) > 0 {
				return normalizeMatrixEntries(value.Rows)
			}
		case *struct {
			Matrix []rawMatrixEntry `json:"matrix"`
		}:
			if len(value.Matrix) > 0 {
				return normalizeMatrixEntries(value.Matrix)
			}
		}
	}

	return nil, errors.New("matrix must be a YAML list or contain a top-level entries/rows/matrix list")
}

func normalizeMatrixEntries(rawEntries []rawMatrixEntry) ([]matrixEntry, error) {
	entries := make([]matrixEntry, 0, len(rawEntries))
	for _, rawEntry := range rawEntries {
		notes, err := normalizeNotes(rawEntry.Notes)
		if err != nil {
			return nil, fmt.Errorf("entry %q notes: %w", rawEntry.Name, err)
		}
		entries = append(entries, matrixEntry{
			Name:          strings.TrimSpace(rawEntry.Name),
			Category:      strings.TrimSpace(rawEntry.Category),
			Status:        strings.TrimSpace(rawEntry.Status),
			EvidencePaths: normalizeStringSlice(rawEntry.EvidencePaths),
			Notes:         notes,
		})
	}
	return entries, nil
}

func normalizeNotes(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return normalizeStringSlice([]string{typed}), nil
	case []any:
		notes := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("must be a string or list of strings")
			}
			notes = append(notes, text)
		}
		return normalizeStringSlice(notes), nil
	default:
		return nil, errors.New("must be a string or list of strings")
	}
}

func normalizeStringSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func loadReleaseNote(path string) (releaseNote, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseNote{}, fmt.Errorf("read release note %s: %w", path, err)
	}
	return parseReleaseNote(string(data))
}

func parseReleaseNote(markdown string) (releaseNote, error) {
	sections := map[string][]markdownBullet{}
	current := ""
	for rawLine := range strings.SplitSeq(markdown, "\n") {
		line := strings.TrimSpace(rawLine)
		if matches := headingPattern.FindStringSubmatch(line); matches != nil {
			current = strings.TrimSpace(matches[2])
			if _, exists := sections[current]; !exists {
				sections[current] = nil
			}
			continue
		}
		if current == "" {
			continue
		}
		if bullet, ok := parseBullet(rawLine); ok {
			sections[current] = append(sections[current], bullet)
		}
	}
	if len(sections) == 0 {
		return releaseNote{}, errors.New("release note must contain markdown headings")
	}
	return releaseNote{sections: sections}, nil
}

func validate(entries []matrixEntry, notes releaseNote) error {
	var issues []string

	experimentalEntries := make([]matrixEntry, 0)
	seenNames := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entryLabel := fmt.Sprintf("%q", entry.Name)
		if entry.Name == "" {
			issues = append(issues, "matrix entry name is required")
			entryLabel = "<unnamed>"
		}
		if entry.Name != "" {
			if _, exists := seenNames[entry.Name]; exists {
				issues = append(issues, fmt.Sprintf("matrix entry %q is duplicated", entry.Name))
			}
			seenNames[entry.Name] = struct{}{}
		}

		if entry.Category == "" {
			issues = append(issues, fmt.Sprintf("matrix entry %s category is required", entryLabel))
		}
		if !slices.Contains([]string{statusSupported, statusExperimental}, entry.Status) {
			issues = append(issues, fmt.Sprintf("matrix entry %s status must be %q or %q", entryLabel, statusSupported, statusExperimental))
		}
		if len(entry.EvidencePaths) == 0 {
			issues = append(issues, fmt.Sprintf("matrix entry %s evidencePaths must contain at least one path", entryLabel))
		}
		for _, evidencePath := range entry.EvidencePaths {
			if !filepath.IsAbs(evidencePath) && !strings.HasPrefix(evidencePath, "docs/") && !strings.HasPrefix(evidencePath, "http://") && !strings.HasPrefix(evidencePath, "https://") {
				issues = append(issues, fmt.Sprintf("matrix entry %s evidencePath %q must be a repo path or URL", entryLabel, evidencePath))
				continue
			}
			if strings.HasPrefix(evidencePath, "docs/") {
				if _, err := os.Stat(evidencePath); err != nil {
					issues = append(issues, fmt.Sprintf("matrix entry %s evidencePath %q must exist in the repository", entryLabel, evidencePath))
				}
			}
		}
		if len(entry.Notes) == 0 {
			issues = append(issues, fmt.Sprintf("matrix entry %s notes must contain at least one item", entryLabel))
		}
		if entry.Status == statusExperimental {
			experimentalEntries = append(experimentalEntries, entry)
		}
	}

	experimentalHeading, experimentalBullets, ok := findExperimentalBullets(notes.sections)
	if !ok {
		issues = append(issues, "release note must describe Experimental entries")
	} else {
		experimentalText := normalizeForMatch(joinBulletTexts(experimentalBullets))
		for _, entry := range experimentalEntries {
			if !strings.Contains(experimentalText, normalizeForMatch(entry.Name)) {
				issues = append(issues, fmt.Sprintf("experimental matrix entry %q must be listed in release note section %q", entry.Name, experimentalHeading))
			}
		}
	}

	knownConstraintsHeading, knownConstraintsBullets, ok := findSection(notes.sections, "Known Constraints", "既知制約")
	if !ok {
		issues = append(issues, "release note section \"Known Constraints\" or \"既知制約\" is required")
	} else {
		knownConstraintsText := normalizeForMatch(joinBulletTexts(knownConstraintsBullets))
		for _, rule := range requiredKnownConstraints {
			matched := true
			for _, group := range rule.keywordGroups {
				groupMatched := false
				for _, keyword := range group {
					if strings.Contains(knownConstraintsText, normalizeForMatch(keyword)) {
						groupMatched = true
						break
					}
				}
				if !groupMatched {
					matched = false
					break
				}
			}
			if !matched {
				issues = append(issues, fmt.Sprintf("release note section %q must describe %s", knownConstraintsHeading, rule.name))
			}
		}
	}

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func findSection(sections map[string][]markdownBullet, names ...string) (string, []markdownBullet, bool) {
	for heading, bullets := range sections {
		for _, name := range names {
			if strings.EqualFold(strings.TrimSpace(heading), name) {
				return heading, bullets, true
			}
		}
	}
	return "", nil, false
}

func findExperimentalBullets(sections map[string][]markdownBullet) (string, []markdownBullet, bool) {
	if heading, bullets, ok := findSection(sections, experimentalSectionName); ok {
		return heading, bullets, true
	}
	for _, summaryHeading := range []string{"公開状態の要約", "Summary"} {
		heading, bullets, ok := findSection(sections, summaryHeading)
		if !ok {
			continue
		}
		for index, bullet := range bullets {
			if normalizeForMatch(bullet.text) != normalizeForMatch(experimentalSectionName) {
				continue
			}
			nested := make([]markdownBullet, 0)
			for nestedIndex := index + 1; nestedIndex < len(bullets); nestedIndex++ {
				if bullets[nestedIndex].indent <= bullet.indent {
					break
				}
				nested = append(nested, bullets[nestedIndex])
			}
			if len(nested) > 0 {
				return heading + " > " + experimentalSectionName, nested, true
			}
		}
	}
	return "", nil, false
}

func parseBullet(line string) (markdownBullet, bool) {
	trimmedLeft := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmedLeft, "- ") && !strings.HasPrefix(trimmedLeft, "* ") {
		return markdownBullet{}, false
	}
	return markdownBullet{
		indent: len(line) - len(trimmedLeft),
		text:   strings.TrimSpace(trimmedLeft[2:]),
	}, true
}

func joinBulletTexts(bullets []markdownBullet) string {
	texts := make([]string, 0, len(bullets))
	for _, bullet := range bullets {
		texts = append(texts, bullet.text)
	}
	return strings.Join(texts, "\n")
}

func normalizeForMatch(value string) string {
	lower := strings.ToLower(value)
	replacer := strings.NewReplacer(
		"`", "",
		"*", " ",
		"_", " ",
		"-", " ",
		"/", " ",
		"(", " ",
		")", " ",
		",", " ",
		".", " ",
		":", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(lower)), " ")
}
