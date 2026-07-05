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
	corev1 "k8s.io/api/core/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func BootMACAddress(host *infrastructurev1alpha1.TartHost) string {
	if host.Spec.BootMACAddress != "" {
		return host.Spec.BootMACAddress
	}
	return host.Spec.MACAddress
}

func RefForHost(host *infrastructurev1alpha1.TartHost) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartHost",
		Namespace:  host.Namespace,
		Name:       host.Name,
		UID:        host.UID,
	}
}

func RefForMachine(machine *infrastructurev1alpha1.TartMachine) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartMachine",
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
}

func MachineRefMatches(ref *corev1.ObjectReference, machine *infrastructurev1alpha1.TartMachine) bool {
	if ref == nil {
		return false
	}
	if ref.Name != machine.Name || ref.Namespace != machine.Namespace {
		return false
	}
	return ref.UID == "" || machine.UID == "" || ref.UID == machine.UID
}

func MachineRefIndexValue(ref *corev1.ObjectReference) string {
	if ref == nil {
		return ""
	}
	return ref.Namespace + "/" + ref.Name + "/" + string(ref.UID)
}

func MachineRefIndexValueForMachine(machine *infrastructurev1alpha1.TartMachine) string {
	return machine.Namespace + "/" + machine.Name + "/" + string(machine.UID)
}
