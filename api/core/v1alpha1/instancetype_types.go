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

// InstanceTypeSpec defines the desired state of InstanceType
type InstanceTypeSpec struct {
	ProviderRef string `json:"providerRef"`
}

// InstanceTypeStatus defines the observed state of InstanceType.
type InstanceTypeStatus struct {
	Region         string                 `json:"region"`
	Zones          []ZoneInstanceTypeSpec `json:"zones"`
	Generation     int64                  `json:"generation,omitempty"`
	RunnerPodName  string                 `json:"runnerPodName,omitempty"`
	NeedsRerun     bool                   `json:"needsRerun,omitempty"`
	LastUpdateTime metav1.Time            `json:"lastUpdateTime,omitempty"`
	LastRunPhase   string                 `json:"lastRunPhase,omitempty"`
	LastRunReason  string                 `json:"lastRunReason,omitempty"`
	LastRunMessage string                 `json:"lastRunMessage,omitempty"`
	Conditions     []metav1.Condition     `json:"conditions,omitempty"`
}

type ZoneInstanceTypeSpec struct {
	ZoneName string             `json:"zone"`
	Flavors  []InstanceOffering `json:"flavors"`
}

type InstanceOffering struct {
	Name        string   `json:"name"`
	NameLabel   string   `json:"nameLabel"`
	VCPUs       int      `json:"vcpus"`
	RAM         string   `json:"ram"`
	Price       string   `json:"price,omitempty"`
	GPU         GPU      `json:"gpu,omitempty"`
	Generation  string   `json:"generation,omitempty"`
	VolumeTypes []string `json:"volumeTypes,omitempty"`
	Spot        Spot     `json:"spot,omitempty"`
}

type GPU struct {
	Enabled      bool   `json:"enabled"`
	Manufacturer string `json:"manufacturer"`
	Count        int    `json:"count"`
	Model        string `json:"model"`
	Memory       string `json:"memory"`
}

type Spot struct {
	Price   string `json:"price"`
	Enabled bool   `json:"enabled"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// InstanceType is the Schema for the instancetypes API
type InstanceType struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of InstanceType
	// +required
	Spec InstanceTypeSpec `json:"spec"`

	// status defines the observed state of InstanceType
	// +optional
	Status InstanceTypeStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// InstanceTypeList contains a list of InstanceType
type InstanceTypeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstanceType `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InstanceType{}, &InstanceTypeList{})
}
