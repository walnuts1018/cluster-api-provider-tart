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

package v1alpha1

import (
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

// ConvertToはTartMachineをv1beta1へ変換する。
func (src *TartMachine) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrastructurev1beta1.TartMachine)
	restored := &infrastructurev1beta1.TartMachine{}
	if ok, err := utilconversion.UnmarshalData(src, restored); err != nil {
		return err
	} else if ok {
		*dst = *restored
	}

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec.ProviderID = src.Spec.ProviderID
	dst.Spec.Image.Ref = legacyImageReference(src.Spec.Image)
	if dst.Spec.PlatformProfile == "" {
		dst.Spec.PlatformProfile = legacyPlatformProfile
	}
	if dst.Spec.DeletionPolicy == "" {
		dst.Spec.DeletionPolicy = infrastructurev1beta1.DeletionPolicyWipeAll
	}
	if dst.Spec.UpdatePolicy.Mode == "" {
		dst.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeReplace
	}
	provisioned := src.Status.Initialization.Provisioned
	dst.Status.Initialization.Provisioned = &provisioned
	dst.Status.FailureDomain = src.Spec.FailureDomain
	dst.Status.HostRef = objectReferenceToResourceReference(src.Status.HostRef)
	dst.Status.Addresses = make(clusterv1.MachineAddresses, 0, len(src.Status.Addresses))
	for _, address := range src.Status.Addresses {
		dst.Status.Addresses = append(dst.Status.Addresses, clusterv1.MachineAddress{
			Type:    clusterv1.MachineAddressType(address.Type),
			Address: address.Address,
		})
	}
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	return nil
}

// ConvertFromはTartMachineをv1beta1から変換する。
func (dst *TartMachine) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrastructurev1beta1.TartMachine)

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec.ProviderID = src.Spec.ProviderID
	dst.Spec.FailureDomain = src.Status.FailureDomain
	dst.Spec.Image = src.Spec.Image.Ref
	dst.Status.Initialization.Provisioned = src.Status.Initialization.Provisioned != nil &&
		*src.Status.Initialization.Provisioned
	dst.Status.Ready = dst.Status.Initialization.Provisioned
	dst.Status.HostRef = resourceReferenceToObjectReference(src.Status.HostRef, "TartHost")
	dst.Status.Addresses = make([]TartMachineAddress, 0, len(src.Status.Addresses))
	for _, address := range src.Status.Addresses {
		dst.Status.Addresses = append(dst.Status.Addresses, TartMachineAddress{
			Type:    coreAddressType(address.Type),
			Address: address.Address,
		})
	}
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	return utilconversion.MarshalData(src, dst)
}
