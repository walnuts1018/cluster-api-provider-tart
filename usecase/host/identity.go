package host

import (
	"strings"

	"uuid"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

// HasIdentityConflictはMAC addressまたはmaintenance Talosが観測したsystem UUIDが別Hostと重複しているかを判定する。空のidentityは未観測として重複扱いにしない。
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
		if host.Status.Inventory != nil && other.Status.Inventory != nil &&
			diskIdentityConflict(host.Status.Inventory.Disks, other.Status.Inventory.Disks) {
			return true
		}
	}
	return false
}

// HasDiskIdentityConflictはこのHostが他のHostまたは自身のinventory内でdisk identity(WWIDまたはserial)を重複して報告しているかだけを判定する。MAC addressやsystem UUIDの重複は対象外である。
func HasDiskIdentityConflict(host infrav1alpha1.TartHost, hosts []infrav1alpha1.TartHost) bool {
	if host.Status.Inventory != nil && diskIdentityConflictWithin(host.Status.Inventory.Disks) {
		return true
	}
	for _, other := range hosts {
		if other.Name == host.Name && other.Namespace == host.Namespace {
			continue
		}
		if host.Status.Inventory != nil && other.Status.Inventory != nil &&
			diskIdentityConflict(host.Status.Inventory.Disks, other.Status.Inventory.Disks) {
			return true
		}
	}
	return false
}

// HasIdentityConflictForAnyはinventory全体で重複したstable identityが存在するかを判定する。重複したHostを一台だけ除外すると誤ったHostへのconfiguration applyを許すため、全体を停止する。
func HasIdentityConflictForAny(hosts []infrav1alpha1.TartHost) bool {
	for index, candidate := range hosts {
		if candidate.Status.Inventory != nil && diskIdentityConflictWithin(candidate.Status.Inventory.Disks) {
			return true
		}
		if HasIdentityConflict(candidate, hosts[index+1:]) || HasIdentityConflict(candidate, hosts[:index]) {
			return true
		}
	}
	return false
}

func diskIdentityConflict(left, right []infrav1alpha1.DiskInventory) bool {
	for _, leftDisk := range left {
		for _, rightDisk := range right {
			if sameDiskIdentity(leftDisk, rightDisk) {
				return true
			}
		}
	}
	return false
}

func diskIdentityConflictWithin(disks []infrav1alpha1.DiskInventory) bool {
	for index, disk := range disks {
		if diskIdentityConflict([]infrav1alpha1.DiskInventory{disk}, disks[index+1:]) {
			return true
		}
	}
	return false
}

func sameDiskIdentity(left, right infrav1alpha1.DiskInventory) bool {
	leftWWID := strings.TrimSpace(left.WWID)
	rightWWID := strings.TrimSpace(right.WWID)
	if leftWWID != "" && rightWWID != "" && strings.EqualFold(leftWWID, rightWWID) {
		return true
	}
	leftSerial := strings.TrimSpace(left.Serial)
	rightSerial := strings.TrimSpace(right.Serial)
	return leftSerial != "" && rightSerial != "" && leftSerial == rightSerial
}

func sameIdentity(left, right string) bool {
	leftUUID, leftErr := uuid.Parse(left)
	rightUUID, rightErr := uuid.Parse(right)
	return leftErr == nil && rightErr == nil && leftUUID != uuid.Nil() && leftUUID == rightUUID
}

func sameMACAddress(left, right network.MACAddress) bool {
	return !left.IsZero() && !right.IsZero() && left == right
}
