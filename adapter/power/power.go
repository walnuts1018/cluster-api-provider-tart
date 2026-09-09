// Package powerはHostの電源操作に関するportとbackend選択のFactoryを提供する。
// 実装はboot配下の旧電源backendをadapter層へ統合し、新しいbackendはサブパッケージとして追加し、Factoryのswitchへcaseを追加するだけで拡張できる。
// 汎用的なregistryやplugin frameworkは導入せず、明示的なFactoryに留める。
package power

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power/intelmanageability"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power/redfish"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power/wol"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/power"
)

// FactoryはTartHostSpecから適切な電源backendを生成する。RedfishのようにSecretを要するbackendはclientとmanagementNamespaceを使って解決する。
func Factory(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (any, error) {
	if host == nil {
		return nil, errors.New("tart host is unavailable")
	}
	switch host.Spec.Power.Backend {
	case infrav1alpha1.PowerBackendWakeOnLAN:
		if host.Spec.Power.WakeOnLAN == nil {
			return nil, errors.New("wake-on-LAN power configuration is missing")
		}
		if host.Spec.MACAddress.IsZero() {
			return nil, errors.New("wake-on-LAN requires a host MAC address")
		}
		return wol.New(host.Spec.MACAddress, host.Spec.Power.WakeOnLAN.BroadcastAddress)
	case infrav1alpha1.PowerBackendRedfish:
		return NewRedfishBackend(ctx, reader, managementNamespace, host)
	case infrav1alpha1.PowerBackendIntelManageability:
		return NewIntelManageabilityBackend(ctx, reader, managementNamespace, host)
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
	powerOn, ok := backend.(power.PowerOn)
	if !ok {
		return fmt.Errorf("power backend %q does not support PowerOn", host.Spec.Power.Backend)
	}
	return powerOn.PowerOn(ctx)
}

// PowerOffHostはHostの電源停止をFactory経由で実行する。controllerの薄いラッパである。
// Redfish/IntelManageabilityのように独立したpower-state observerを持つbackendに対して、
// Talos API経由のgraceful shutdownが利用できない場合(maintenance modeはShutdown RPCを
// 提供しない等)のout-of-band fallbackとして使う。
func PowerOffHost(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) error {
	backend, err := Factory(ctx, reader, managementNamespace, host)
	if err != nil {
		return err
	}
	powerOff, ok := backend.(power.PowerOff)
	if !ok {
		return fmt.Errorf("power backend %q does not support PowerOff", host.Spec.Power.Backend)
	}
	return powerOff.PowerOff(ctx)
}

// RedfishPowerStateはRedfish backendの電源状態を取得する。
func RedfishPowerState(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (power.PowerState, error) {
	backend, err := NewRedfishBackend(ctx, reader, managementNamespace, host)
	if err != nil {
		return power.PowerStateUnknown, err
	}
	return backend.PowerState(ctx)
}

// IntelManageabilityPowerStateはIntel Manageability backendの電源状態を取得する。
func IntelManageabilityPowerState(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (power.PowerState, error) {
	backend, err := NewIntelManageabilityBackend(ctx, reader, managementNamespace, host)
	if err != nil {
		return power.PowerStateUnknown, err
	}
	return backend.PowerState(ctx)
}

// backendCredentialsはRedfish/Intel Manageability backendに共通する、Secretから解決したcredentialとCA materialである。
type backendCredentials struct {
	username string
	password string
	caData   []byte
}

// resolveBackendCredentialsは、out-of-band power backendが共通して必要とするusername/password credential Secretと
// 任意のCA Secretをprovider管理namespaceから解決する。backendNameはerror messageにのみ使う識別用文字列である。
func resolveBackendCredentials(ctx context.Context, reader client.Reader, managementNamespace, backendName string, credentialSecretRef infrav1alpha1.ManagementNamespaceSecretReference, caSecretRef *infrav1alpha1.ManagementNamespaceSecretReference) (backendCredentials, error) {
	if reader == nil {
		return backendCredentials{}, fmt.Errorf("kubernetes client is unavailable for %s credentials", backendName)
	}
	if strings.TrimSpace(managementNamespace) == "" {
		return backendCredentials{}, fmt.Errorf("provider management namespace is not configured for %s credentials", backendName)
	}
	if strings.TrimSpace(credentialSecretRef.Name) == "" {
		return backendCredentials{}, fmt.Errorf("%s credential Secret name is empty", backendName)
	}
	credentialSecret := &corev1.Secret{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: managementNamespace, Name: credentialSecretRef.Name}, credentialSecret); err != nil {
		return backendCredentials{}, fmt.Errorf("get %s credential Secret: %w", backendName, err)
	}
	username, usernameOK := credentialSecret.Data["username"]
	password, passwordOK := credentialSecret.Data["password"]
	if !usernameOK || strings.TrimSpace(string(username)) == "" || !passwordOK || strings.TrimSpace(string(password)) == "" {
		return backendCredentials{}, fmt.Errorf("%s credential Secret must contain non-empty username and password keys", backendName)
	}

	var caData []byte
	if caSecretRef != nil {
		if strings.TrimSpace(caSecretRef.Name) == "" {
			return backendCredentials{}, fmt.Errorf("%s CA Secret name is empty", backendName)
		}
		caSecret := &corev1.Secret{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: managementNamespace, Name: caSecretRef.Name}, caSecret); err != nil {
			return backendCredentials{}, fmt.Errorf("get %s CA Secret: %w", backendName, err)
		}
		var ok bool
		caData, ok = caSecret.Data["ca.crt"]
		if !ok || len(caData) == 0 {
			return backendCredentials{}, fmt.Errorf("%s CA Secret must contain a non-empty ca.crt key", backendName)
		}
	}

	return backendCredentials{username: string(username), password: string(password), caData: caData}, nil
}

// NewIntelManageabilityBackendはIntel Manageability credential Secretを解決してbackendを構築する。
func NewIntelManageabilityBackend(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (*intelmanageability.Backend, error) {
	if host == nil {
		return nil, errors.New("tart host is unavailable")
	}
	config := host.Spec.Power.IntelManageability
	if config == nil {
		return nil, errors.New("intel manageability power configuration is missing")
	}
	credentials, err := resolveBackendCredentials(ctx, reader, managementNamespace, "Intel Manageability", config.CredentialSecretRef, config.CASecretRef)
	if err != nil {
		return nil, err
	}
	return intelmanageability.New(intelmanageability.Config{
		Address:            config.Address,
		Username:           credentials.username,
		Password:           credentials.password,
		CAData:             credentials.caData,
		InsecureSkipVerify: config.InsecureSkipVerify,
	})
}

// NewRedfishBackendはRedfish credential Secretを解決してbackendを構築する。旧controller/power.goのbuildRedfishBackendと同等の責務を持つ。
func NewRedfishBackend(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (*redfish.Backend, error) {
	if host == nil {
		return nil, errors.New("tart host is unavailable")
	}
	config := host.Spec.Power.Redfish
	if config == nil {
		return nil, errors.New("redfish power configuration is missing")
	}
	credentials, err := resolveBackendCredentials(ctx, reader, managementNamespace, "Redfish", config.CredentialSecretRef, config.CASecretRef)
	if err != nil {
		return nil, err
	}
	return redfish.New(redfish.Config{
		Address:            config.Address,
		SystemID:           config.SystemID,
		Username:           credentials.username,
		Password:           credentials.password,
		CAData:             credentials.caData,
		InsecureSkipVerify: config.InsecureSkipVerify,
	})
}
