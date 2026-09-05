// Package v1alpha1 contains the Tart Bootstrap Provider API
// (bootstrap.cluster.x-k8s.io/v1alpha1): TartBootstrapConfig, TartBootstrapConfigTemplate.
//
// +kubebuilder:object:generate=true
// +groupName=bootstrap.cluster.x-k8s.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "bootstrap.cluster.x-k8s.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&TartBootstrapConfig{},
		&TartBootstrapConfigList{},
		&TartBootstrapConfigTemplate{},
		&TartBootstrapConfigTemplateList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
