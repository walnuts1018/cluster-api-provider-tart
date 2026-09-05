// Package v1alpha1はTart Bootstrap Provider API(bootstrap.cluster.x-k8s.io/v1alpha1)のTartBootstrapConfigとTartBootstrapConfigTemplateを定義する。
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
	// GroupVersionはこれらのオブジェクトを登録するgroup versionである。
	GroupVersion = schema.GroupVersion{Group: "bootstrap.cluster.x-k8s.io", Version: "v1alpha1"}

	// SchemeBuilderはGo型をGroupVersionKind schemeへ追加するために使う。
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToSchemeはこのgroup versionの型を指定されたschemeへ追加する。
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
