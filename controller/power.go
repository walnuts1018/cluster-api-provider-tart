package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/boot"
)

func (r *TartHostReconciler) powerOnHost(ctx context.Context, host *infrav1alpha1.TartHost) error {
	switch host.Spec.Power.Backend {
	case infrav1alpha1.PowerBackendWakeOnLAN:
		if host.Spec.Power.WakeOnLAN == nil {
			return errors.New("wake-on-LAN power configuration is missing")
		}
		backend, err := boot.NewWakeOnLAN(host.Spec.MACAddress, host.Spec.Power.WakeOnLAN.BroadcastAddress)
		if err != nil {
			return err
		}
		return backend.PowerOn(ctx)
	case infrav1alpha1.PowerBackendRedfish:
		backend, err := r.redfishBackend(ctx, host)
		if err != nil {
			return err
		}
		return backend.PowerOn(ctx)
	case infrav1alpha1.PowerBackendManual:
		return errors.New("manual power backend cannot power on through the normal path")
	default:
		return fmt.Errorf("host power backend %q cannot power on through the normal path", host.Spec.Power.Backend)
	}
}

func (r *TartMachineReconciler) redfishPowerState(ctx context.Context, host *infrav1alpha1.TartHost) (boot.PowerState, error) {
	backend, err := r.redfishBackend(ctx, host)
	if err != nil {
		return boot.PowerStateUnknown, err
	}
	return backend.PowerState(ctx)
}

func (r *TartHostReconciler) redfishBackend(ctx context.Context, host *infrav1alpha1.TartHost) (*boot.Redfish, error) {
	return buildRedfishBackend(ctx, r.Client, r.ManagementNamespace, host)
}

func (r *TartMachineReconciler) redfishBackend(ctx context.Context, host *infrav1alpha1.TartHost) (*boot.Redfish, error) {
	return buildRedfishBackend(ctx, r.Client, r.ManagementNamespace, host)
}

func buildRedfishBackend(ctx context.Context, reader client.Reader, managementNamespace string, host *infrav1alpha1.TartHost) (*boot.Redfish, error) {
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

	return boot.NewRedfish(boot.RedfishConfig{
		Address:            config.Address.String(),
		SystemID:           config.SystemID,
		Username:           string(username),
		Password:           string(password),
		CAData:             caData,
		InsecureSkipVerify: config.InsecureSkipVerify,
	})
}
