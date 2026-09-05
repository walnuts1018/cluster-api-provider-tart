// resolver.goは、PXE bootしてきたHostのMACアドレスから、controller-managerが管理する
// TartHost/TartMachineのdesired Talos imageをread-onlyで解決する。netboot-server自身は
// Secretを読まず、Status/Conditionを書かず、Kubernetes API以外の永続状態を持たない。
package netboot

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

// BootImageは、あるMACアドレスからのPXE bootリクエストに対して配信すべきTalos imageの解決結果である。
type BootImage struct {
	// Versionは"v"始まりのTalos versionである。
	Version string
	// SchematicIDはTalos Image Factoryのschematic identifierである。
	SchematicID string
}

// HostImageResolverは、MACアドレスからPXEクライアントへ配信すべきTalos imageを解決する。
type HostImageResolver interface {
	// ResolveBootImageは指定のMACアドレスに対応するBootImageを返す。
	// このMACアドレスに対応するTartHostが存在しないか、TartHostがまだ稼働中のMachineへ
	// claimされていない場合はfoundがfalseになり、呼び出し側はdiscovery用の既定imageへfallbackする。
	ResolveBootImage(ctx context.Context, mac string) (image BootImage, found bool, err error)
}

// TartHostImageResolverは、Kubernetes APIをread-onlyで参照してBootImageを解決するHostImageResolverである。
// TartHost/TartMachineのget/list/watchだけが必要であり、Secretへのアクセス権限は要求しない。
type TartHostImageResolver struct {
	reader client.Reader
}

// NewTartHostImageResolverは新しいTartHostImageResolverを作成する。
func NewTartHostImageResolver(reader client.Reader) (*TartHostImageResolver, error) {
	if reader == nil {
		return nil, errors.New("reader is required")
	}
	return &TartHostImageResolver{reader: reader}, nil
}

// ResolveBootImageは、macに一致するspec.macAddressを持つTartHostを探し、そのconsumerRefが
// 指すTartMachineのspec.imageを返す。TartHostが見つからない、consumerRefが未設定、または
// consumerRefが指すTartMachineが存在しない場合はfound=falseを返し、呼び出し側のdiscovery
// fallbackへ委ねる(fresh machineの初回enrollment boot、またはHost登録前の状態に対応するため)。
func (r *TartHostImageResolver) ResolveBootImage(ctx context.Context, mac string) (BootImage, bool, error) {
	normalized, err := network.ParseMACAddress(mac)
	if err != nil {
		return BootImage{}, false, fmt.Errorf("parse MAC address: %w", err)
	}

	var hosts infrav1alpha1.TartHostList
	if err := r.reader.List(ctx, &hosts); err != nil {
		return BootImage{}, false, fmt.Errorf("list TartHost: %w", err)
	}

	var host *infrav1alpha1.TartHost
	for index := range hosts.Items {
		if hosts.Items[index].Spec.MACAddress == normalized {
			host = &hosts.Items[index]
			break
		}
	}
	if host == nil {
		return BootImage{}, false, nil
	}

	consumerRef := host.Spec.ConsumerRef
	if consumerRef == nil || consumerRef.Kind != "TartMachine" || consumerRef.Name == "" {
		return BootImage{}, false, nil
	}

	machine := &infrav1alpha1.TartMachine{}
	key := client.ObjectKey{Namespace: consumerRef.Namespace, Name: consumerRef.Name}
	if err := r.reader.Get(ctx, key, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return BootImage{}, false, nil
		}
		return BootImage{}, false, fmt.Errorf("get TartMachine: %w", err)
	}
	if machine.Spec.Image.Version == "" || machine.Spec.Image.SchematicID == "" {
		return BootImage{}, false, nil
	}

	return BootImage{Version: machine.Spec.Image.Version, SchematicID: machine.Spec.Image.SchematicID}, true, nil
}

// noopHostImageResolverは、Kubernetes APIへの接続が設定されていない場合に使う既定のresolverであり、
// 常にfound=falseを返してdiscovery用imageへfallbackさせる。
type noopHostImageResolver struct{}

func (noopHostImageResolver) ResolveBootImage(context.Context, string) (BootImage, bool, error) {
	return BootImage{}, false, nil
}
