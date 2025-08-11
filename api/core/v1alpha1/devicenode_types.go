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

// DeviceNodeSpec defines the desired state of DeviceNode
type DeviceNodeSpec struct {
	ProviderRef string       `json:"providerRef"`
	DeviceSpec  []DeviceSpec `json:"deviceSpec"`
}

// DeviceNodeStatus defines the observed state of DeviceNode.
type DeviceNodeStatus struct {
	Region      string       `json:"region"`
	DevicesSpec []DeviceSpec `json:"devicesSpec"`
}

type DeviceSpec struct {
	ZoneRef string    `json:"zoneRef"`
	COREs   int       `json:"cores"`
	RAM     string    `json:"ram"`
	GPU     DeviceGPU `json:"gpu"`
}

type DeviceGPU struct {
	Enabled      bool   `json:"enabled"`
	Manufacturer string `json:"manufacturer"`
	Count        int    `json:"count"`
	Model        string `json:"model"`
	Memory       string `json:"memory"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DeviceNode is the Schema for the devicenodes API
type DeviceNode struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of DeviceNode
	// +required
	Spec DeviceNodeSpec `json:"spec"`

	// status defines the observed state of DeviceNode
	// +optional
	Status DeviceNodeStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// DeviceNodeList contains a list of DeviceNode
type DeviceNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeviceNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DeviceNode{}, &DeviceNodeList{})
}
