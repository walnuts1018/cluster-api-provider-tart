package bootstrap

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// DiskSelectorは、1つのDiskIdentityから導出したstableなTalos CEL disk selector候補である。
// install target(UnattendedInstallConfig)、VolumeConfig、UserVolumeConfig、RawVolumeConfig、
// LVMVolumeGroupConfigのいずれのdisk selectorも、このSelectorを共通の基盤として構築する。
// 実際のcel.Expressionへの変換(構文解析・環境の適用)はadapter/talos/configbuilderが行う。
type DiskSelector struct {
	// Expressionは、Talos DiskLocator/VolumeLocator CEL環境の両方で評価可能な文字列表現である。
	Expression string
	matches    func(DiskIdentity) bool
}

// DiskSelectorsForは、指定したdiskを識別する候補selectorを、より具体的なもの(WWID、serial、bus path)から
// 順に返す。呼び出し側は候補の中から観測済みinventory全体に対して一意に一致するものを選ぶ。
func DiskSelectorsFor(disk DiskIdentity) []DiskSelector {
	baseExpression := []string{
		`!disk.readonly`,
		"disk.size == " + strconv.FormatUint(disk.SizeBytes, 10) + "u",
	}
	baseMatches := func(candidate DiskIdentity) bool {
		return !candidate.ReadOnly && candidate.SizeBytes == disk.SizeBytes
	}
	selectors := make([]DiskSelector, 0, 4)

	if strings.TrimSpace(disk.WWID) != "" {
		selectors = append(selectors, DiskSelector{
			Expression: strings.Join(append(slices.Clone(baseExpression), `disk.wwid == `+strconv.Quote(disk.WWID)), " && "),
			matches: func(candidate DiskIdentity) bool {
				return baseMatches(candidate) && candidate.WWID == disk.WWID
			},
		})
	}

	if strings.TrimSpace(disk.Serial) != "" {
		expression := append(slices.Clone(baseExpression), `disk.serial == `+strconv.Quote(disk.Serial))
		matches := func(candidate DiskIdentity) bool {
			return baseMatches(candidate) && candidate.Serial == disk.Serial
		}
		if strings.TrimSpace(disk.Model) != "" {
			expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
			previousMatches := matches
			matches = func(candidate DiskIdentity) bool {
				return previousMatches(candidate) && candidate.Model == disk.Model
			}
		}
		selectors = append(selectors, DiskSelector{Expression: strings.Join(expression, " && "), matches: matches})
	}

	if strings.TrimSpace(disk.BusPath) != "" {
		expression := append(slices.Clone(baseExpression), `disk.bus_path == `+strconv.Quote(disk.BusPath))
		matches := func(candidate DiskIdentity) bool {
			return baseMatches(candidate) && candidate.BusPath == disk.BusPath
		}
		if strings.TrimSpace(disk.Model) != "" {
			expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
			previousMatches := matches
			matches = func(candidate DiskIdentity) bool {
				return previousMatches(candidate) && candidate.Model == disk.Model
			}
		}
		selectors = append(selectors, DiskSelector{Expression: strings.Join(expression, " && "), matches: matches})
	}

	expression := slices.Clone(baseExpression)
	matches := baseMatches
	if strings.TrimSpace(disk.Model) != "" {
		expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
		previousMatches := matches
		matches = func(candidate DiskIdentity) bool {
			return previousMatches(candidate) && candidate.Model == disk.Model
		}
	}
	if disk.Rotational {
		expression = append(expression, `disk.rotational`)
		previousMatches := matches
		matches = func(candidate DiskIdentity) bool {
			return previousMatches(candidate) && candidate.Rotational
		}
	} else {
		expression = append(expression, `!disk.rotational`)
		previousMatches := matches
		matches = func(candidate DiskIdentity) bool {
			return previousMatches(candidate) && !candidate.Rotational
		}
	}
	selectors = append(selectors, DiskSelector{Expression: strings.Join(expression, " && "), matches: matches})

	return selectors
}

// UniqueDiskSelectorは、candidates(通常は同じHostで観測した全disk)の中でdiskを一意に識別できる
// 最も具体的なselector expressionを返す。一意に識別できない場合はok=falseを返す。
// TartHost.status.inventoryのdisk一覧を表示する際、ユーザーがWWID等の生値を読み取って比較しなくても
// 「この選択でこのdiskだけが一致する」ことを確認できるようにするpreview用途にも使う。
func UniqueDiskSelector(disk DiskIdentity, candidates []DiskIdentity) (string, bool) {
	for _, selector := range DiskSelectorsFor(disk) {
		matches := 0
		for _, candidate := range candidates {
			if selector.matches(candidate) {
				matches++
			}
		}
		if matches == 1 {
			return selector.Expression, true
		}
	}
	return "", false
}

// SelectDiskは、観測したdisk群から書き込み可能なcandidateを絞り込み、stableなselectorで一意に識別できる
// diskだけを返す。boot orderに依存する/dev/sdXなどへfallbackしない。install target、追加volume、
// LVM member diskのいずれの選択にも共通して使う。
func SelectDisk(disks []DiskIdentity) (DiskIdentity, error) {
	candidates := make([]DiskIdentity, 0, len(disks))
	for _, disk := range disks {
		if strings.TrimSpace(disk.DevicePath) == "" || disk.SizeBytes == 0 || disk.ReadOnly {
			continue
		}
		candidates = append(candidates, disk)
	}
	if len(candidates) == 0 {
		return DiskIdentity{}, ErrDiskSelectionUnavailable
	}
	slices.SortFunc(candidates, func(left, right DiskIdentity) int {
		return cmp.Compare(left.DevicePath, right.DevicePath)
	})

	for _, candidate := range candidates {
		if _, ok := UniqueDiskSelector(candidate, candidates); ok {
			return candidate, nil
		}
	}

	return DiskIdentity{}, ErrDiskSelectionAmbiguous
}
