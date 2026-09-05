// Package v1alpha1はTart Control Plane Provider API(controlplane.cluster.x-k8s.io/v1alpha1)のTartControlPlaneとTartControlPlaneTemplateを定義する。
//
// +kubebuilder:object:generate=true
// +groupName=controlplane.cluster.x-k8s.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersionはこれらのオブジェクトを登録するgroup versionである。
	GroupVersion = schema.GroupVersion{Group: "controlplane.cluster.x-k8s.io", Version: "v1alpha1"}

	// SchemeBuilderはGo型をGroupVersionKind schemeへ追加するために使う。
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToSchemeはこのgroup versionの型を指定されたschemeへ追加する。
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&TartControlPlane{},
		&TartControlPlaneList{},
		&TartControlPlaneTemplate{},
		&TartControlPlaneTemplateList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
