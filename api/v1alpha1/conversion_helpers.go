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
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

const (
	legacyPlatformProfile = "legacy-v1alpha1/v1"
	migrationRegistry     = "migration.invalid"
)

func legacyImageReference(image string) string {
	digest := sha256.Sum256([]byte(image))
	return fmt.Sprintf("oci://%s/legacy@sha256:%x", migrationRegistry, digest)
}

func resourceReferenceToObjectReference(ref *infrastructurev1beta1.ResourceReference, kind string) *corev1.ObjectReference {
	if ref == nil {
		return nil
	}
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1beta1.GroupVersion.String(),
		Kind:       kind,
		Namespace:  ref.Namespace,
		Name:       ref.Name,
		UID:        ref.UID,
	}
}

func objectReferenceToResourceReference(ref *corev1.ObjectReference) *infrastructurev1beta1.ResourceReference {
	if ref == nil {
		return nil
	}
	return &infrastructurev1beta1.ResourceReference{
		Namespace: ref.Namespace,
		Name:      ref.Name,
		UID:       ref.UID,
	}
}

func coreAddressType(addressType clusterv1.MachineAddressType) corev1.NodeAddressType {
	return corev1.NodeAddressType(addressType)
}

func templateMetadataToV1Beta2(metadata metav1.ObjectMeta) clusterv1.ObjectMeta {
	return clusterv1.ObjectMeta{
		Labels:      metadata.Labels,
		Annotations: metadata.Annotations,
	}
}

func templateMetadataToV1Alpha1(metadata clusterv1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Labels:      metadata.Labels,
		Annotations: metadata.Annotations,
	}
}
