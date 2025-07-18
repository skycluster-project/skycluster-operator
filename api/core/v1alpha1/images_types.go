/*
Copyright 2025.

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

// ImagesSpec defines the desired state of Images
type ImagesSpec struct {
	ProviderRef string          `json:"providerRef"`
	Zones       []ImageOffering `json:"zones"`
}

// ImagesStatus defines the observed state of Images.
type ImagesStatus struct {
	Region string          `json:"region"`
	Zones  []ImageOffering `json:"zones"`
}

type ImageOffering struct {
	// +kubebuilder:validation:Enum=aws;ubuntu2004;ubuntu2204;ubuntu2404;eksoptimized
	NameLabel string `json:"nameLabel"`
	ZoneName  string `json:"zoneName"`
	Name      string `json:"name,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Images is the Schema for the images API
type Images struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Images
	// +required
	Spec ImagesSpec `json:"spec"`

	// status defines the observed state of Images
	// +optional
	Status ImagesStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ImagesList contains a list of Images
type ImagesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Images `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Images{}, &ImagesList{})
}
