package configbuilder

import (
	"bytes"
	"fmt"
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
func EnsureInstallDisk(configuration []byte, disk domainbootstrap.DiskIdentity) ([]byte, error) {
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

	selector, err := domainbootstrap.SelectDisk([]domainbootstrap.DiskIdentity{disk})
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

func validateInstallDiskConfiguration(provider talosconfig.Provider, disk domainbootstrap.DiskIdentity) error {
	if provider == nil || strings.TrimSpace(disk.DevicePath) == "" || disk.SizeBytes == 0 || disk.ReadOnly {
		return domainbootstrap.ErrInstallDiskUnavailable
	}

	expected, err := diskExpression(disk, celenv.DiskLocator())
	if err != nil {
		return fmt.Errorf("%w: parse provider install disk selector: %w", domainbootstrap.ErrInstallConfigurationInvalid, err)
	}

	if unattended := provider.UnattendedInstallConfig(); unattended != nil {
		if err := validateInstallSelector(unattended.VolumeSelector()); err != nil {
			return err
		}
		if unattended.VolumeWipe() {
			return fmt.Errorf("%w: unattended install target enables volume wipe", domainbootstrap.ErrInstallConfigurationInvalid)
		}
		matches, err := equivalentSelectors(unattended.VolumeSelector(), expected)
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

// equivalentSelectorsは2つのCEL selectorが同一のexpressionを表すかを、両方をpretty-printした文字列表現の
// 比較によって判定する。
func equivalentSelectors(actual, expected cel.Expression) (bool, error) {
	actualText, err := actual.MarshalText()
	if err != nil {
		return false, fmt.Errorf("%w: encode Talos disk selector: %w", domainbootstrap.ErrInstallConfigurationInvalid, err)
	}
	actualParsed, err := cel.ParseBooleanExpression(string(actualText), celenv.DiskLocator())
	if err != nil {
		return false, fmt.Errorf("%w: parse Talos disk selector: %w", domainbootstrap.ErrInstallConfigurationInvalid, err)
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

func unattendedInstallPatch(disk domainbootstrap.DiskIdentity) (talosconfig.Provider, error) {
	expression, err := diskExpression(disk, celenv.DiskLocator())
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
