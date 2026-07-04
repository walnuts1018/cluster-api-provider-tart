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
	"k8s.io/utils/ptr"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

// ConvertToはTartClusterをv1beta1へ変換する。
func (src *TartCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrastructurev1beta1.TartCluster)
	restored := &infrastructurev1beta1.TartCluster{}
	if ok, err := utilconversion.UnmarshalData(src, restored); err != nil {
		return err
	} else if ok {
		*dst = *restored
	}

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec.ControlPlaneEndpoint.Host = src.Spec.ControlPlaneEndpoint.Host
	dst.Spec.ControlPlaneEndpoint.Port = src.Spec.ControlPlaneEndpoint.Port
	if len(dst.Spec.ArtifactPolicy.AllowedRegistries) == 0 {
		dst.Spec.ArtifactPolicy.AllowedRegistries = []string{migrationRegistry}
	}
	dst.Status.Initialization.Provisioned = new(src.Status.Initialization.Provisioned)
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.Conditions = src.Status.Conditions
	return nil
}

// ConvertFromはTartClusterをv1beta1から変換する。
func (dst *TartCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrastructurev1beta1.TartCluster)

	dst.ObjectMeta = *src.ObjectMeta.DeepCopy()
	dst.Spec.ControlPlaneEndpoint.Host = src.Spec.ControlPlaneEndpoint.Host
	dst.Spec.ControlPlaneEndpoint.Port = src.Spec.ControlPlaneEndpoint.Port
	dst.Status.Initialization.Provisioned = ptr.Deref(src.Status.Initialization.Provisioned, false)
	dst.Status.Ready = dst.Status.Initialization.Provisioned
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.Conditions = src.Status.Conditions
	return utilconversion.MarshalData(src, dst)
}
