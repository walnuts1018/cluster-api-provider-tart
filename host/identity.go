package host

import (
	"errors"
	"uuid"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

var ErrInvalidHostID = errors.New("invalid host id")

// ProviderIDはTartHost.spec.idから決定論的に生成する。metadata.uidやHost名には依存しない。
func ProviderID(id hostdomain.HostID) (hostdomain.ProviderID, error) {
	providerID, err := hostdomain.NewProviderID(id)
	if err != nil {
		return hostdomain.ProviderID(""), ErrInvalidHostID
	}
	return providerID, nil
}

// HasIdentityConflictはMAC addressまたはmaintenance Talosが観測したsystem UUIDが
// 別Hostと重複しているかを判定する。空のidentityは未観測として重複扱いにしない。
func HasIdentityConflict(host infrav1alpha1.TartHost, hosts []infrav1alpha1.TartHost) bool {
	for _, other := range hosts {
		if other.Name == host.Name && other.Namespace == host.Namespace {
			continue
		}
		if sameMACAddress(host.Spec.MACAddress, other.Spec.MACAddress) {
			return true
		}
		if host.Status.Inventory != nil && other.Status.Inventory != nil &&
			sameIdentity(host.Status.Inventory.SystemUUID, other.Status.Inventory.SystemUUID) {
			return true
		}
	}
	return false
}

// HasIdentityConflictForAnyはinventory全体で重複したstable identityが存在するかを判定する。
// 重複したHostを一台だけ除外すると誤ったHostへのconfiguration applyを許すため、全体を停止する。
func HasIdentityConflictForAny(hosts []infrav1alpha1.TartHost) bool {
	for index, candidate := range hosts {
		if HasIdentityConflict(candidate, hosts[index+1:]) || HasIdentityConflict(candidate, hosts[:index]) {
			return true
		}
	}
	return false
}

func sameIdentity(left, right uuid.UUID) bool {
	return left != uuid.Nil() && right != uuid.Nil() && left == right
}

func sameMACAddress(left, right network.MACAddress) bool {
	return !left.IsZero() && !right.IsZero() && left == right
}
