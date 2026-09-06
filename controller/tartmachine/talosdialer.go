package controller

import (
	"context"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	recovery "github.com/walnuts1018/cluster-api-provider-tart/usecase/recovery"
)

// TalosNodeはReprovision flowが必要とするTalos APIの観測と破壊的操作だけを表す。
// 実機のTalos APIを必要とする経路をここへ閉じ込め、reconcileのpolicy部分をGo testから検証できるようにする。
type TalosNode interface {
	// Inventoryは接続中nodeのhardware identityを観測する。
	Inventory(ctx context.Context) (talos.Inventory, error)
	// ActiveMachineConfigurationは接続中nodeへ現在適用されているmachine configurationを観測する。
	ActiveMachineConfiguration(ctx context.Context) ([]byte, error)
	// ResetはTalos installationのsystem partitionを消去してmaintenance modeへ戻す。
	Reset(ctx context.Context) error
	// Closeは接続を解放する。
	Close() error
}

// TalosDialerはReprovision flowのTalos接続確立を抽象化する。
type TalosDialer interface {
	// DialRecoveryはrecovery CAから発行した短命な`os:admin` client certificateで旧Talos APIへ認証済み接続を確立する。
	// server certificateがrecovery CAに属することの検証はTLS handshakeで行われるため、接続成功自体が旧cluster identityの暗号学的な証明になる。
	DialRecovery(ctx context.Context, endpoint string, material recovery.Material) (TalosNode, error)
	// DialMaintenanceは未構成nodeのmaintenance APIへ接続する。認証されないため、識別はinventoryの照合だけに使う。
	DialMaintenance(ctx context.Context, endpoint string) (TalosNode, error)
}

// defaultTalosDialerは実際のTalos gRPC clientでTalosDialerを満たす。
type defaultTalosDialer struct{}

func (defaultTalosDialer) DialRecovery(ctx context.Context, endpoint string, material recovery.Material) (TalosNode, error) {
	certificate, err := material.ClientCertificate(recovery.ClientCertificateTTL)
	if err != nil {
		return nil, err
	}
	client, err := talos.DialAuthenticated(ctx, endpoint, certificate.Crt, certificate.Key, material.CertificateAuthority.Crt)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (defaultTalosDialer) DialMaintenance(ctx context.Context, endpoint string) (TalosNode, error) {
	client, err := talos.DialMaintenance(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (r *TartMachineReconciler) talosDialer() TalosDialer {
	if r.TalosDialer != nil {
		return r.TalosDialer
	}
	return defaultTalosDialer{}
}
