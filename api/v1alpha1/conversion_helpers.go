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
