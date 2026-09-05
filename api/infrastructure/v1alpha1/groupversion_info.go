// Package v1alpha1 contains the Tart Infrastructure Provider API
// (infrastructure.cluster.x-k8s.io/v1alpha1): TartHost, TartCluster,
// TartClusterTemplate, TartMachine, TartMachineTemplate.
//
// +kubebuilder:object:generate=true
// +groupName=infrastructure.cluster.x-k8s.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "infrastructure.cluster.x-k8s.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&TartCluster{},
		&TartClusterList{},
		&TartClusterTemplate{},
		&TartClusterTemplateList{},
		&TartHost{},
		&TartHostList{},
		&TartMachine{},
		&TartMachineList{},
		&TartMachineTemplate{},
		&TartMachineTemplateList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
