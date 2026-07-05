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
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

// ConvertToはTartMachineTemplateをv1beta1へ変換する。
func (src *TartMachineTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrastructurev1beta1.TartMachineTemplate)
	restored := &infrastructurev1beta1.TartMachineTemplate{}
	if ok, err := utilconversion.UnmarshalData(src, restored); err != nil {
		return err
	} else if ok {
		*dst = *restored
	}

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec.Template.ObjectMeta = templateMetadataToV1Beta2(src.Spec.Template.ObjectMeta)
	dst.Spec.Template.Spec.Image.Ref = legacyImageReference(src.Spec.Template.Spec.Image)
	if dst.Spec.Template.Spec.PlatformProfile == "" {
		dst.Spec.Template.Spec.PlatformProfile = legacyPlatformProfile
	}
	if dst.Spec.Template.Spec.DeletionPolicy == "" {
		dst.Spec.Template.Spec.DeletionPolicy = infrastructurev1beta1.DeletionPolicyWipeAll
	}
	if dst.Spec.Template.Spec.UpdatePolicy.Mode == "" {
		dst.Spec.Template.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeReplace
	}
	return nil
}

// ConvertFromはTartMachineTemplateをv1beta1から変換する。
func (dst *TartMachineTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrastructurev1beta1.TartMachineTemplate)

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec.Template.ObjectMeta = templateMetadataToV1Alpha1(src.Spec.Template.ObjectMeta)
	dst.Spec.Template.Spec.Image = src.Spec.Template.Spec.Image.Ref
	return utilconversion.MarshalData(src, dst)
}
