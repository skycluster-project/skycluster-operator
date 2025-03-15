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

type SkyService struct {
	Name       string             `json:"name,omitempty"`
	Kind       string             `json:"kind,omitempty"`
	APIVersion string             `json:"apiVersion,omitempty"`
	Manifest   string             `json:"manifest,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// SkyXRDSpec defines the desired state of SkyXRD.
type SkyXRDSpec struct {
	// Manifests is a list of manifests to apply to the cluster
	Manifests []SkyService `json:"manifests,omitempty"`
}

// SkyXRDStatus defines the observed state of SkyXRD.
type SkyXRDStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SkyXRD is the Schema for the skyxrds API.
type SkyXRD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkyXRDSpec   `json:"spec,omitempty"`
	Status SkyXRDStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SkyXRDList contains a list of SkyXRD.
type SkyXRDList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyXRD `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyXRD{}, &SkyXRDList{})
}
