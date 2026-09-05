package bootstrap

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/cel"
	"github.com/siderolabs/talos/pkg/machinery/cel/celenv"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	runtimeconfig "github.com/siderolabs/talos/pkg/machinery/config/types/runtime"
	v1alpha1config "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
)

var (
	ErrInstallDiskUnavailable      = errors.New("no install disk is available")
	ErrInstallDiskAmbiguous        = errors.New("install disk cannot be selected unambiguously")
	ErrInstallConfigurationInvalid = errors.New("machine configuration install target is invalid")
)

// InstallDiskはTalosのsystem disk選択に必要な非機密hardware observationである。DevicePathは旧Talos設定形式で使用し、現行設定形式では生成したstable selectorを使用する。
type InstallDisk struct {
	DevicePath string
	SizeBytes  uint64
	Model      string
	Serial     string
	WWID       string
	BusPath    string
	Transport  string
	Rotational bool
	ReadOnly   bool
}

type diskSelector struct {
	expression string
	matches    func(InstallDisk) bool
}

// SelectInstallDiskは現在観測した書き込み可能なphysical diskへstableなTalos selectorを適用し、一意に一致する場合だけ選択する。boot orderに依存する/dev/sdXなどへfallbackしない。
func SelectInstallDisk(disks []InstallDisk) (InstallDisk, error) {
	candidates := make([]InstallDisk, 0, len(disks))
	for _, disk := range disks {
		if strings.TrimSpace(disk.DevicePath) == "" || disk.SizeBytes == 0 || disk.ReadOnly {
			continue
		}
		candidates = append(candidates, disk)
	}
	if len(candidates) == 0 {
		return InstallDisk{}, ErrInstallDiskUnavailable
	}
	slices.SortFunc(candidates, func(left, right InstallDisk) int {
		return cmp.Compare(left.DevicePath, right.DevicePath)
	})

	for _, candidate := range candidates {
		for _, selector := range selectorsFor(candidate) {
			matches := 0
			for _, disk := range candidates {
				if selector.matches(disk) {
					matches++
				}
			}
			if matches == 1 {
				return candidate, nil
			}
		}
	}

	return InstallDisk{}, ErrInstallDiskAmbiguous
}

func selectorsFor(disk InstallDisk) []diskSelector {
	baseExpression := []string{
		`!disk.readonly`,
		"disk.size == " + strconv.FormatUint(disk.SizeBytes, 10) + "u",
	}
	baseMatches := func(candidate InstallDisk) bool {
		return !candidate.ReadOnly && candidate.SizeBytes == disk.SizeBytes
	}
	selectors := make([]diskSelector, 0, 4)

	if strings.TrimSpace(disk.WWID) != "" {
		selectors = append(selectors, diskSelector{
			expression: strings.Join(append(slices.Clone(baseExpression), `disk.wwid == `+strconv.Quote(disk.WWID)), " && "),
			matches: func(candidate InstallDisk) bool {
				return baseMatches(candidate) && candidate.WWID == disk.WWID
			},
		})
	}

	if strings.TrimSpace(disk.Serial) != "" {
		expression := append(slices.Clone(baseExpression), `disk.serial == `+strconv.Quote(disk.Serial))
		matches := func(candidate InstallDisk) bool {
			return baseMatches(candidate) && candidate.Serial == disk.Serial
		}
		if strings.TrimSpace(disk.Model) != "" {
			expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
			previousMatches := matches
			matches = func(candidate InstallDisk) bool {
				return previousMatches(candidate) && candidate.Model == disk.Model
			}
		}
		selectors = append(selectors, diskSelector{expression: strings.Join(expression, " && "), matches: matches})
	}

	if strings.TrimSpace(disk.BusPath) != "" {
		expression := append(slices.Clone(baseExpression), `disk.bus_path == `+strconv.Quote(disk.BusPath))
		matches := func(candidate InstallDisk) bool {
			return baseMatches(candidate) && candidate.BusPath == disk.BusPath
		}
		if strings.TrimSpace(disk.Model) != "" {
			expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
			previousMatches := matches
			matches = func(candidate InstallDisk) bool {
				return previousMatches(candidate) && candidate.Model == disk.Model
			}
		}
		selectors = append(selectors, diskSelector{expression: strings.Join(expression, " && "), matches: matches})
	}

	expression := slices.Clone(baseExpression)
	matches := baseMatches
	if strings.TrimSpace(disk.Model) != "" {
		expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
		previousMatches := matches
		matches = func(candidate InstallDisk) bool {
			return previousMatches(candidate) && candidate.Model == disk.Model
		}
	}
	if disk.Rotational {
		expression = append(expression, `disk.rotational`)
		previousMatches := matches
		matches = func(candidate InstallDisk) bool {
			return previousMatches(candidate) && candidate.Rotational
		}
	} else {
		expression = append(expression, `!disk.rotational`)
		previousMatches := matches
		matches = func(candidate InstallDisk) bool {
			return previousMatches(candidate) && !candidate.Rotational
		}
	}
	selectors = append(selectors, diskSelector{expression: strings.Join(expression, " && "), matches: matches})

	return selectors
}

// HasInstallDiskConfigurationはconfigurationがinstall targetを特定済みか返す。不正なunattended documentはprovider生成値で黙って置換せず、errorとして返す。
func HasInstallDiskConfiguration(configuration []byte) (bool, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return false, ErrCompleteConfigurationEmpty
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return false, fmt.Errorf("load Talos machine configuration: %w", err)
	}
	if unattended := provider.UnattendedInstallConfig(); unattended != nil {
		if unattended.VolumeSelector().IsZero() {
			return false, ErrInstallConfigurationInvalid
		}
		return true, nil
	}

	machine := provider.Machine()
	if machine == nil {
		return false, nil
	}
	install := machine.Install()
	if install == nil {
		return false, nil
	}
	if strings.TrimSpace(install.Disk()) != "" {
		return true, nil
	}
	expression, err := install.DiskMatchExpression()
	if err != nil {
		return false, fmt.Errorf("read Talos install disk selector: %w", err)
	}
	return expression != nil && !expression.IsZero(), nil
}

// EnsureInstallDiskは入力configurationにinstall targetがない場合、Talos nativeのinstall targetを追加する。
// 現行Talos設定にはUnattendedInstallConfig documentを追加し、旧設定にはmachine.install.diskを設定する。
func EnsureInstallDisk(configuration []byte, disk InstallDisk) ([]byte, error) {
	configured, err := HasInstallDiskConfiguration(configuration)
	if err != nil {
		return nil, err
	}
	if configured {
		return bytes.Clone(configuration), nil
	}
	if strings.TrimSpace(disk.DevicePath) == "" {
		return nil, ErrInstallDiskUnavailable
	}

	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load Talos machine configuration: %w", err)
	}
	if raw := provider.RawV1Alpha1(); raw != nil && raw.MachineConfig != nil && raw.MachineConfig.MachineInstall != nil { //nolint:staticcheck // legacy configuration support.
		patched, patchErr := provider.PatchV1Alpha1(func(config *v1alpha1config.Config) error {
			if config.MachineConfig.MachineInstall == nil { //nolint:staticcheck // legacy configuration support.
				config.MachineConfig.MachineInstall = &v1alpha1config.InstallConfig{} //nolint:staticcheck // legacy configuration support.
			}
			config.MachineConfig.MachineInstall.InstallDisk = disk.DevicePath //nolint:staticcheck // legacy configuration support.
			config.MachineConfig.MachineInstall.InstallWipe = new(false)      //nolint:staticcheck // preserve data outside the selected system disk.
			return nil
		})
		if patchErr != nil {
			return nil, fmt.Errorf("set legacy Talos install disk: %w", patchErr)
		}
		return patched.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	}

	selector, err := SelectInstallDisk([]InstallDisk{disk})
	if err != nil {
		return nil, err
	}
	patchProvider, err := unattendedInstallPatch(selector)
	if err != nil {
		return nil, err
	}
	output, err := configpatcher.Apply(configpatcher.WithBytes(configuration), []configpatcher.Patch{
		configpatcher.NewStrategicMergePatch(patchProvider),
	})
	if err != nil {
		return nil, fmt.Errorf("apply Talos unattended install disk selector: %w", err)
	}
	result, err := output.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode Talos unattended install disk selector: %w", err)
	}
	canonical, err := configloader.NewFromBytes(result)
	if err != nil {
		return nil, fmt.Errorf("load Talos configuration with install disk selector: %w", err)
	}
	return canonical.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
}

func unattendedInstallPatch(disk InstallDisk) (talosconfig.Provider, error) {
	selector := selectorsFor(disk)
	if len(selector) == 0 {
		return nil, ErrInstallDiskUnavailable
	}
	expression, err := cel.ParseBooleanExpression(selector[0].expression, celenv.DiskLocator())
	if err != nil {
		return nil, fmt.Errorf("parse generated Talos install disk selector: %w", err)
	}
	unattended := runtimeconfig.NewUnattendedInstallConfigV1Alpha1()
	unattended.ProvisioningSpec.DiskSelector.Match = expression
	unattended.ProvisioningSpec.Wipe = new(false)
	patchProvider, err := container.New(unattended)
	if err != nil {
		return nil, fmt.Errorf("build generated Talos install disk selector: %w", err)
	}
	return patchProvider, nil
}
