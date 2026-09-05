// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package host

import (
	"errors"
	"strings"
	"uuid"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

var ErrInvalidHostID = errors.New("invalid host id")

// ProviderIDはTartHost.spec.idから決定論的に生成する。metadata.uidやHost名には依存しない。
func ProviderID(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil() {
		return "", ErrInvalidHostID
	}
	return "tart://host/" + parsed.String(), nil
}

// HasIdentityConflictはMAC addressまたはmaintenance Talosが観測したsystem UUIDが
// 別Hostと重複しているかを判定する。空のidentityは未観測として重複扱いにしない。
func HasIdentityConflict(host infrav1alpha1.TartHost, hosts []infrav1alpha1.TartHost) bool {
	for _, other := range hosts {
		if other.Name == host.Name && other.Namespace == host.Namespace {
			continue
		}
		if sameIdentity(host.Spec.MACAddress, other.Spec.MACAddress) {
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

func sameIdentity(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left != "" && right != "" && strings.EqualFold(left, right)
}
