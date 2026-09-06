// Package powerはHostの電源操作に関するportとbackend選択のFactoryを提供する。
// 実装はboot配下の旧電源backendをadapter層へ統合し、新しいbackend(fakeを含む)はサブパッケージとして追加し、Factoryのswitchへcaseを追加するだけで拡張できる。
// 汎用的なregistryやplugin frameworkは導入せず、明示的なFactoryに留める。
package power

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power/redfish"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power/wol"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

// PowerStateは電源backendが観測したHostの電源状態である。
type PowerState string

const (
	PowerStateOn      PowerState = "On"
	PowerStateOff     PowerState = "Off"
	PowerStateUnknown PowerState = "Unknown"
)

// PowerOnはHostの電源投入を要求する。
type PowerOn interface {
	PowerOn(ctx context.Context) error
}

// PowerOffはHostの安全な電源停止を要求する。
type PowerOff interface {
	PowerOff(ctx context.Context) error
}

// PowerStateObserverはHostの電源状態を観測する。
type PowerStateObserver interface {
	PowerState(ctx context.Context) (PowerState, error)
}

// BackendはHostに設定されたpower backend種別を表す。
type Backend string

const (
	BackendWakeOnLAN Backend = "WakeOnLAN"
	BackendRedfish   Backend = "Redfish"
	BackendManual    Backend = "Manual"
	BackendFake      Backend = "Fake"
)

// FactoryはTartHostSpecから適切な電源backendを生成する。RedfishのようにSecretを要するbackendはclientとmanagementNamespaceを使って解決する。
// Fake backendはテストでのみ使用する。
func Factory(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (any, error) {
	if host == nil {
		return nil, errors.New("tart host is unavailable")
	}
	switch host.Spec.Power.Backend {
	case infrav1alpha1.PowerBackendWakeOnLAN:
		if host.Spec.Power.WakeOnLAN == nil {
			return nil, errors.New("wake-on-LAN power configuration is missing")
		}
		mac, err := network.ParseMACAddress(host.Spec.MACAddress.String())
		if err != nil {
			return nil, fmt.Errorf("validate wake-on-LAN MAC address: %w", err)
		}
		return wol.New(mac, host.Spec.Power.WakeOnLAN.BroadcastAddress)
	case infrav1alpha1.PowerBackendRedfish:
		return NewRedfishBackend(ctx, reader, managementNamespace, host)
	case infrav1alpha1.PowerBackendManual:
		return nil, errors.New("manual power backend cannot power on through the normal path")
	default:
		return nil, fmt.Errorf("host power backend %q cannot power on through the normal path", host.Spec.Power.Backend)
	}
}

// PowerOnHostはHostの電源投入をFactory経由で実行する。controllerの薄いラッパである。
func PowerOnHost(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) error {
	backend, err := Factory(ctx, reader, managementNamespace, host)
	if err != nil {
		return err
	}
	powerOn, ok := backend.(PowerOn)
	if !ok {
		return fmt.Errorf("power backend %q does not support PowerOn", host.Spec.Power.Backend)
	}
	return powerOn.PowerOn(ctx)
}

// RedfishPowerStateはRedfish backendの電源状態を取得する。
func RedfishPowerState(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (PowerState, error) {
	backend, err := NewRedfishBackend(ctx, reader, managementNamespace, host)
	if err != nil {
		return PowerStateUnknown, err
	}
	state, err := backend.PowerState(ctx)
	if err != nil {
		return PowerStateUnknown, err
	}
	return PowerState(state), nil
}

// NewRedfishBackendはRedfish credential Secretを解決してbackendを構築する。旧controller/power.goのbuildRedfishBackendと同等の責務を持つ。
func NewRedfishBackend(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (*redfish.Backend, error) {
	if reader == nil {
		return nil, errors.New("kubernetes client is unavailable for Redfish credentials")
	}
	if strings.TrimSpace(managementNamespace) == "" {
		return nil, errors.New("provider management namespace is not configured for Redfish credentials")
	}
	config := host.Spec.Power.Redfish
	if config == nil {
		return nil, errors.New("redfish power configuration is missing")
	}
	if strings.TrimSpace(config.CredentialSecretRef.Name) == "" {
		return nil, errors.New("redfish credential Secret name is empty")
	}
	credentialSecret := &corev1.Secret{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: managementNamespace, Name: config.CredentialSecretRef.Name}, credentialSecret); err != nil {
		return nil, fmt.Errorf("get Redfish credential Secret: %w", err)
	}
	username, usernameOK := credentialSecret.Data["username"]
	password, passwordOK := credentialSecret.Data["password"]
	if !usernameOK || strings.TrimSpace(string(username)) == "" || !passwordOK || strings.TrimSpace(string(password)) == "" {
		return nil, errors.New("redfish credential Secret must contain non-empty username and password keys")
	}

	var caData []byte
	if config.CASecretRef != nil {
		if strings.TrimSpace(config.CASecretRef.Name) == "" {
			return nil, errors.New("redfish CA Secret name is empty")
		}
		caSecret := &corev1.Secret{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: managementNamespace, Name: config.CASecretRef.Name}, caSecret); err != nil {
			return nil, fmt.Errorf("get Redfish CA Secret: %w", err)
		}
		var ok bool
		caData, ok = caSecret.Data["ca.crt"]
		if !ok || len(caData) == 0 {
			return nil, errors.New("redfish CA Secret must contain a non-empty ca.crt key")
		}
	}

	return redfish.New(redfish.Config{
		Address:            config.Address.String(),
		SystemID:           config.SystemID,
		Username:           string(username),
		Password:           string(password),
		CAData:             caData,
		InsecureSkipVerify: config.InsecureSkipVerify,
	})
}
