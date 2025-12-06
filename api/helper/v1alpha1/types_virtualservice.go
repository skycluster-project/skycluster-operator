package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// VirtualService represents a virtual service offered by a cloud provider
// an example would be an ComputeProfile offered as a virtual machine
// instance types.
type VirtualService struct {
	// Name specifies the name of virtual service, if applicable
	Name string       `json:"name,omitempty"`
	// Spec specifies the spec of virtual service in yaml/json format, if applicable.
	// The spec structure is according to the kind of virtual service.
	Spec *runtime.RawExtension  	 		`json:"spec,omitempty"`
	// +kubebuilder:validation:Enum=ComputeProfile
	Kind string       `json:"kind,omitempty"`
	metav1.TypeMeta `json:",inline"`
}

// SkyService represents a service supported by SkyCluster compositions
// It references a component (defined by user) and include the manifests
// needed to be deployed with the refrenced provider.
type SkyService struct {
	Name      string            `json:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	// ComponentRef references a component in deployment policy object
	// defined by user
	ComponentRef ComponentRef `json:"componentRef"`
	// Manifest is a manifest of a SkyCluster composition API object
	Manifest     string                 `json:"manifest,omitempty"`
	ProviderRef  ProviderRefSpec        `json:"providerRef,omitempty"`
	Conditions   []metav1.Condition     `json:"conditions,omitempty"`
}
