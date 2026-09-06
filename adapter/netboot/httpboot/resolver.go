package httpboot

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

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
func (r *TartHostImageResolver) ResolveBootImage(ctx context.Context, mac string) (domainnetboot.BootImage, bool, error) {
	normalized, err := network.ParseMACAddress(mac)
	if err != nil {
		return domainnetboot.BootImage{}, false, fmt.Errorf("parse MAC address: %w", err)
	}

	var hosts infrav1alpha1.TartHostList
	if err := r.reader.List(ctx, &hosts); err != nil {
		return domainnetboot.BootImage{}, false, fmt.Errorf("list TartHost: %w", err)
	}

	var host *infrav1alpha1.TartHost
	for index := range hosts.Items {
		if hosts.Items[index].Spec.MACAddress == normalized {
			host = &hosts.Items[index]
			break
		}
	}
	if host == nil {
		return domainnetboot.BootImage{}, false, nil
	}

	consumerRef := host.Spec.ConsumerRef
	if consumerRef == nil || consumerRef.Kind != "TartMachine" || consumerRef.Name == "" {
		return domainnetboot.BootImage{}, false, nil
	}

	machine := &infrav1alpha1.TartMachine{}
	key := client.ObjectKey{Namespace: consumerRef.Namespace, Name: consumerRef.Name}
	if err := r.reader.Get(ctx, key, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return domainnetboot.BootImage{}, false, nil
		}
		return domainnetboot.BootImage{}, false, fmt.Errorf("get TartMachine: %w", err)
	}
	if machine.Spec.Image.Version == "" || machine.Spec.Image.SchematicID == "" {
		return domainnetboot.BootImage{}, false, nil
	}

	return domainnetboot.BootImage{Version: machine.Spec.Image.Version, SchematicID: machine.Spec.Image.SchematicID}, true, nil
}
