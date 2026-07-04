/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

// ConvertToはTartHostをv1beta1へ変換する。
func (src *TartHost) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrastructurev1beta1.TartHost)
	restored := &infrastructurev1beta1.TartHost{}
	if ok, err := utilconversion.UnmarshalData(src, restored); err != nil {
		return err
	} else if ok {
		*dst = *restored
	}

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	bootMACAddress := src.Spec.BootMACAddress
	if bootMACAddress == "" {
		bootMACAddress = src.Spec.MACAddress
	}
	dst.Spec.Identifiers.BootMACAddress = bootMACAddress
	if dst.Spec.Architecture == "" {
		dst.Spec.Architecture = infrastructurev1beta1.ArchitectureAMD64
	}
	if dst.Spec.Firmware == "" {
		dst.Spec.Firmware = infrastructurev1beta1.FirmwareUEFI
	}
	if dst.Spec.PlatformProfile == "" {
		dst.Spec.PlatformProfile = legacyPlatformProfile
	}
	if dst.Spec.RootDeviceHints.MinSizeBytes == 0 {
		dst.Spec.RootDeviceHints.MinSizeBytes = 1
	}
	if dst.Spec.Management.PowerDriver == "" {
		dst.Spec.Management.PowerDriver = "wol"
	}
	if dst.Spec.Management.BootDriver == "" {
		dst.Spec.Management.BootDriver = "ipxe"
	}
	dst.Status.Phase = infrastructurev1beta1.TartHostPhase(src.Status.State)
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.Conditions = src.Status.Conditions
	if src.Status.MachineRef != nil {
		dst.Spec.ConsumerRef = &infrastructurev1beta1.ResourceReference{
			Namespace: src.Status.MachineRef.Namespace,
			Name:      src.Status.MachineRef.Name,
			UID:       src.Status.MachineRef.UID,
		}
	}
	return nil
}

// ConvertFromはTartHostをv1beta1から変換する。
func (dst *TartHost) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrastructurev1beta1.TartHost)

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec.MACAddress = src.Spec.Identifiers.BootMACAddress
	dst.Spec.BootMACAddress = src.Spec.Identifiers.BootMACAddress
	dst.Status.State = TartHostState(src.Status.Phase)
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.Conditions = src.Status.Conditions
	if src.Spec.ConsumerRef != nil {
		dst.Status.MachineRef = resourceReferenceToObjectReference(src.Spec.ConsumerRef, "TartMachine")
	}
	return utilconversion.MarshalData(src, dst)
}
