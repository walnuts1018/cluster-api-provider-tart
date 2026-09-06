package controller

import (
	"errors"
	"net"
	"strings"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// ErrHostIdentityMismatchは観測したTalos maintenance identityがTartHostのMACAddressと一致しないことを表す。
// tarthostとtartmachineの両方が、既存Host識別が壊れていないことの確認に使う。
var ErrHostIdentityMismatch = errors.New("talos maintenance identity does not match host")

// ErrHostEndpointUnavailableはTartHostのTalos API endpointを解決できないことを表す。
var ErrHostEndpointUnavailable = errors.New("talos endpoint is unavailable")

// HostTalosEndpointはTartHostへ到達するためのTalos API endpointを、明示指定されたaddressか観測済みaddressから決定する。
// tarthost、tartmachine、tartcontrolplaneの各Reconcilerから共有される。
func HostTalosEndpoint(host *infrav1alpha1.TartHost) string {
	if endpoint := host.Spec.TalosAPIAddress.String(); endpoint != "" {
		return endpoint
	}
	for _, addressType := range []clusterv1.MachineAddressType{clusterv1.MachineInternalIP, clusterv1.MachineExternalIP, clusterv1.MachineHostName} {
		for _, address := range host.Status.Addresses {
			if address.Type == addressType && strings.TrimSpace(address.Address) != "" {
				return strings.TrimSpace(address.Address)
			}
		}
	}
	return ""
}

// HostAddressesはTalos endpoint文字列からCAPI Machine addressesを導出する。
func HostAddresses(endpoint string) clusterv1.MachineAddresses {
	address := endpoint
	if hostPart, _, err := net.SplitHostPort(endpoint); err == nil {
		address = hostPart
	}
	address = strings.Trim(address, "[]")
	if net.ParseIP(address) != nil {
		return clusterv1.MachineAddresses{{Type: clusterv1.MachineInternalIP, Address: address}}
	}
	return clusterv1.MachineAddresses{{Type: clusterv1.MachineHostName, Address: address}}
}
