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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TartClusterSpec defines the desired state of TartCluster.
type TartClusterSpec struct {
	// controlPlaneEndpoint represents the endpoint for the control plane for this cluster.
	// +optional
	ControlPlaneEndpoint EndpointAddress `json:"controlPlaneEndpoint,omitempty"`
}

// EndpointAddress defines the control plane endpoint address.
type EndpointAddress struct {
	// host is the hostname or IP address of the control plane endpoint.
	// +kubebuilder:validation:Required
	// +required
	Host string `json:"host"`

	// port is the port of the control plane endpoint.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`
}

// TartClusterStatus defines the observed state of TartCluster.
type TartClusterStatus struct {
	// initialization tracks the provisioning state of the infrastructure.
	// +optional
	Initialization ClusterInitialization `json:"initialization,omitempty"`

	// ready indicates whether the infrastructure resource is ready.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// observedGeneration is the last spec generation reconciled into status.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the TartCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterInitialization tracks the provisioning state of the infrastructure.
type ClusterInitialization struct {
	// bound indicates whether the Cluster has been assigned its infrastructure yet.
	// In that case during the provisioning process the infrastructure is being provisioned.
	// Once provisioned the control plane nodes will be able to communicate using the control plane endpoint.
	// +optional
	Bound bool `json:"bound,omitempty"`

	// controlPlaneReady indicates whether the control plane is available enough to start scheduling workloads.
	// +optional
	ControlPlaneReady bool `json:"controlPlaneReady,omitempty"`

	// provisioned indicates whether the infrastructure is fully provisioned.
	// +optional
	Provisioned bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=tartclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.metadata.labels.cluster\.x-k8s\.io/cluster-name`,description="Cluster to which this TartCluster belongs"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.ready`,description="Ready status"
// +kubebuilder:printcolumn:name="Provisioned",type=string,JSONPath=`.status.initialization.provisioned`,description="Infrastructure provisioned"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// TartCluster is the Schema for the tartclusters API.
type TartCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TartCluster.
	// +required
	Spec TartClusterSpec `json:"spec"`

	// status defines the observed state of TartCluster.
	// +optional
	Status TartClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TartClusterList contains a list of TartCluster.
type TartClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TartCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TartCluster{}, &TartClusterList{})
}
