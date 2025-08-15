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
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

// ImageSpec defines the desired state of Image
type ImageSpec struct {
	ProviderRef string   `json:"providerRef"`
	Images      []ImageOffering `json:"images,omitempty" yaml:"images,omitempty"`
}

	// ImageStatus defines the observed state of Image.
	type ImageStatus struct {
		Region             string             `json:"region" yaml:"region"`
		Images             []ImageOffering    `json:"images,omitempty" yaml:"images,omitempty"`
		ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
		LastUpdateTime     metav1.Time        `json:"lastUpdateTime,omitempty" yaml:"lastUpdateTime,omitempty"`
		Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	}

type ImageOffering struct {
	// +kubebuilder:validation:Enum=ubuntu-20.04;ubuntu-22.04;ubuntu-24.04;eks-optimized
	NameLabel  string `json:"nameLabel" yaml:"nameLabel"`
	Name       string `json:"name,omitempty" yaml:"name,omitempty"`
	Generation string `json:"generation,omitempty" yaml:"generation,omitempty"`
	Zone       string `json:"zone,omitempty" yaml:"zone,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".status.region"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:subresource:status

// Image is the Schema for the images API
type Image struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Image
	// +required
	Spec ImageSpec `json:"spec"`

	// status defines the observed state of Image
	// +optional
	Status ImageStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ImageList contains a list of Image
type ImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Image `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Image{}, &ImageList{})
}

// helper to set a condition
func (s *ImageStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
