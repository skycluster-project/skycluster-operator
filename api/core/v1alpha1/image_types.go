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
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImageSpec defines the desired state of Image
type ImageSpec struct {
	ProviderRef string          `json:"providerRef"`
	Zones       []ImageOffering `json:"zones"`
}

// ImageStatus defines the observed state of Image.
type ImageStatus struct {
	Region         string             `json:"region"`
	Zones          []ImageOffering    `json:"zones"`
	Generation     int64              `json:"generation,omitempty"`
	RunnerPodName  string             `json:"runnerPodName,omitempty"`
	NeedsRerun     bool               `json:"needsRerun,omitempty"`
	LastUpdateTime metav1.Time        `json:"lastUpdateTime,omitempty"`
	LastRunPhase   string             `json:"lastRunPhase,omitempty"`
	LastRunReason  string             `json:"lastRunReason,omitempty"`
	LastRunMessage string             `json:"lastRunMessage,omitempty"`
	Conditions     []metav1.Condition `json:"conditions,omitempty"`
}

type ImageOffering struct {
	// +kubebuilder:validation:Enum=ubuntu-20.04;ubuntu-22.04;ubuntu-24.04;eks-optimized
	NameLabel  string `json:"nameLabel"`
	Name       string `json:"name,omitempty"`
	Generation string `json:"generation,omitempty"`
	Zone       string `json:"zone"`
}

// +kubebuilder:object:root=true
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
func (s *ImageStatus) SetCondition(t string, status corev1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               t,
		Status:             metav1.ConditionStatus(string(status)),
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: s.Generation,
		LastTransitionTime: metav1.Now(),
	})
}
