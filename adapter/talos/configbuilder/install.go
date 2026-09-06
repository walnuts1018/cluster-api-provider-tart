package configbuilder

import (
	"bytes"
	"cmp"
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

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
)

type diskSelector struct {
	expression string
	matches    func(domainbootstrap.InstallDisk) bool
}

// SelectInstallDiskは現在観測した書き込み可能なphysical diskへstableなTalos selectorを適用し、一意に一致する場合だけ選択する。boot orderに依存する/dev/sdXなどへfallbackしない。
func SelectInstallDisk(disks []domainbootstrap.InstallDisk) (domainbootstrap.InstallDisk, error) {
	candidates := make([]domainbootstrap.InstallDisk, 0, len(disks))
	for _, disk := range disks {
		if strings.TrimSpace(disk.DevicePath) == "" || disk.SizeBytes == 0 || disk.ReadOnly {
			continue
		}
		candidates = append(candidates, disk)
	}
	if len(candidates) == 0 {
		return domainbootstrap.InstallDisk{}, domainbootstrap.ErrInstallDiskUnavailable
	}
	slices.SortFunc(candidates, func(left, right domainbootstrap.InstallDisk) int {
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

	return domainbootstrap.InstallDisk{}, domainbootstrap.ErrInstallDiskAmbiguous
}

func selectorsFor(disk domainbootstrap.InstallDisk) []diskSelector {
	baseExpression := []string{
		`!disk.readonly`,
		"disk.size == " + strconv.FormatUint(disk.SizeBytes, 10) + "u",
	}
	baseMatches := func(candidate domainbootstrap.InstallDisk) bool {
		return !candidate.ReadOnly && candidate.SizeBytes == disk.SizeBytes
	}
	selectors := make([]diskSelector, 0, 4)

	if strings.TrimSpace(disk.WWID) != "" {
		selectors = append(selectors, diskSelector{
			expression: strings.Join(append(slices.Clone(baseExpression), `disk.wwid == `+strconv.Quote(disk.WWID)), " && "),
			matches: func(candidate domainbootstrap.InstallDisk) bool {
				return baseMatches(candidate) && candidate.WWID == disk.WWID
			},
		})
	}

	if strings.TrimSpace(disk.Serial) != "" {
		expression := append(slices.Clone(baseExpression), `disk.serial == `+strconv.Quote(disk.Serial))
		matches := func(candidate domainbootstrap.InstallDisk) bool {
			return baseMatches(candidate) && candidate.Serial == disk.Serial
		}
		if strings.TrimSpace(disk.Model) != "" {
			expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
			previousMatches := matches
			matches = func(candidate domainbootstrap.InstallDisk) bool {
				return previousMatches(candidate) && candidate.Model == disk.Model
			}
		}
		selectors = append(selectors, diskSelector{expression: strings.Join(expression, " && "), matches: matches})
	}

	if strings.TrimSpace(disk.BusPath) != "" {
		expression := append(slices.Clone(baseExpression), `disk.bus_path == `+strconv.Quote(disk.BusPath))
		matches := func(candidate domainbootstrap.InstallDisk) bool {
			return baseMatches(candidate) && candidate.BusPath == disk.BusPath
		}
		if strings.TrimSpace(disk.Model) != "" {
			expression = append(expression, `disk.model == `+strconv.Quote(disk.Model))
			previousMatches := matches
			matches = func(candidate domainbootstrap.InstallDisk) bool {
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
		matches = func(candidate domainbootstrap.InstallDisk) bool {
			return previousMatches(candidate) && candidate.Model == disk.Model
		}
	}
	if disk.Rotational {
		expression = append(expression, `disk.rotational`)
		previousMatches := matches
		matches = func(candidate domainbootstrap.InstallDisk) bool {
			return previousMatches(candidate) && candidate.Rotational
		}
	} else {
		expression = append(expression, `!disk.rotational`)
		previousMatches := matches
		matches = func(candidate domainbootstrap.InstallDisk) bool {
			return previousMatches(candidate) && !candidate.Rotational
		}
	}
	selectors = append(selectors, diskSelector{expression: strings.Join(expression, " && "), matches: matches})

	return selectors
}

// HasInstallDiskConfigurationはconfigurationがinstall targetを特定済みか返す。不正なunattended documentはprovider生成値で黙って置換せず、errorとして返す。
func HasInstallDiskConfiguration(configuration []byte) (bool, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return false, domainbootstrap.ErrCompleteConfigurationEmpty
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return false, fmt.Errorf("load Talos machine configuration: %w", err)
	}
	if unattended := provider.UnattendedInstallConfig(); unattended != nil {
		if err := validateInstallSelector(unattended.VolumeSelector()); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func validateInstallSelector(expression cel.Expression) error {
	if expression.IsZero() {
		return domainbootstrap.ErrInstallConfigurationInvalid
	}
	if err := expression.ParseBool(celenv.DiskLocator()); err != nil {
		return fmt.Errorf("%w: invalid disk selector: %w", domainbootstrap.ErrInstallConfigurationInvalid, err)
	}
	return nil
}

// EnsureInstallDiskは入力configurationにmodern Talos install targetがない場合、UnattendedInstallConfigを追加する。
func EnsureInstallDisk(configuration []byte, disk domainbootstrap.InstallDisk) ([]byte, error) {
	configured, err := HasInstallDiskConfiguration(configuration)
	if err != nil {
		return nil, err
	}
	if configured {
		provider, err := configloader.NewFromBytes(configuration)
		if err != nil {
			return nil, fmt.Errorf("load Talos machine configuration: %w", err)
		}
		if err := validateInstallDiskConfiguration(provider, disk); err != nil {
			return nil, err
		}
		return bytes.Clone(configuration), nil
	}
	if strings.TrimSpace(disk.DevicePath) == "" {
		return nil, domainbootstrap.ErrInstallDiskUnavailable
	}

	selector, err := SelectInstallDisk([]domainbootstrap.InstallDisk{disk})
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

func validateInstallDiskConfiguration(provider talosconfig.Provider, disk domainbootstrap.InstallDisk) error {
	if provider == nil || strings.TrimSpace(disk.DevicePath) == "" || disk.SizeBytes == 0 || disk.ReadOnly {
		return domainbootstrap.ErrInstallDiskUnavailable
	}

	expected, err := expectedInstallSelector(disk)
	if err != nil {
		return err
	}

	if unattended := provider.UnattendedInstallConfig(); unattended != nil {
		if err := validateInstallSelector(unattended.VolumeSelector()); err != nil {
			return err
		}
		if unattended.VolumeWipe() {
			return fmt.Errorf("%w: unattended install target enables volume wipe", domainbootstrap.ErrInstallConfigurationInvalid)
		}
		matches, err := equivalentInstallSelectors(unattended.VolumeSelector(), expected)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("%w: unattended install target does not match the provider-selected disk", domainbootstrap.ErrInstallConfigurationInvalid)
		}
		return nil
	}

	return domainbootstrap.ErrInstallConfigurationInvalid
}

func expectedInstallSelector(disk domainbootstrap.InstallDisk) (cel.Expression, error) {
	selectors := selectorsFor(disk)
	if len(selectors) == 0 {
		return cel.Expression{}, domainbootstrap.ErrInstallDiskUnavailable
	}
	expression, err := cel.ParseBooleanExpression(selectors[0].expression, celenv.DiskLocator())
	if err != nil {
		return cel.Expression{}, fmt.Errorf("%w: parse provider install disk selector: %w", domainbootstrap.ErrInstallConfigurationInvalid, err)
	}
	return expression, nil
}

func equivalentInstallSelectors(actual, expected cel.Expression) (bool, error) {
	actualText, err := actual.MarshalText()
	if err != nil {
		return false, fmt.Errorf("%w: encode Talos install disk selector: %w", domainbootstrap.ErrInstallConfigurationInvalid, err)
	}
	actualParsed, err := cel.ParseBooleanExpression(string(actualText), celenv.DiskLocator())
	if err != nil {
		return false, fmt.Errorf("%w: parse Talos install disk selector: %w", domainbootstrap.ErrInstallConfigurationInvalid, err)
	}
	return actualParsed.String() == expected.String(), nil
}

type clientValidationMode struct{}

func (clientValidationMode) String() string {
	return "client"
}

func (clientValidationMode) RequiresInstall() bool {
	return false
}

func (clientValidationMode) InContainer() bool {
	return false
}

// ValidateMachineConfigurationはBootstrap Secretへ保存する前、またはTalos maintenance APIへ送信する前に完全なconfigurationを検証する。
func ValidateMachineConfiguration(configuration []byte) error {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return domainbootstrap.ErrCompleteConfigurationEmpty
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return fmt.Errorf("load Talos machine configuration: %w", err)
	}
	return validateClientConfiguration(provider)
}

func validateClientConfiguration(provider talosconfig.Provider) error {
	if provider == nil {
		return fmt.Errorf("%w: configuration provider is unavailable", domainbootstrap.ErrEffectiveConfigurationInvalid)
	}
	if _, err := provider.ValidateAsClient(clientValidationMode{}); err != nil {
		return fmt.Errorf("%w: Talos client-side validation failed: %w", domainbootstrap.ErrEffectiveConfigurationInvalid, err)
	}
	return nil
}

func unattendedInstallPatch(disk domainbootstrap.InstallDisk) (talosconfig.Provider, error) {
	selector := selectorsFor(disk)
	if len(selector) == 0 {
		return nil, domainbootstrap.ErrInstallDiskUnavailable
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
