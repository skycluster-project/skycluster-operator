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
	depv1a1 "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/dep"
)

// InstanceTypeSpec defines the desired state of InstanceType
type InstanceTypeSpec struct {
	ProviderRef  string   `json:"providerRef"`
	TypeFamilies []string `json:"typeFamilies,omitempty"`
}

// InstanceTypeStatus defines the observed state of InstanceType.
type InstanceTypeStatus struct {
	Region                    string                 `json:"region,omitempty"`
	Zones                     []ZoneInstanceTypeSpec `json:"zones,omitempty"`
	LastUpdateTime            metav1.Time            `json:"lastUpdateTime,omitempty"`
	ObservedGeneration        int64                  `json:"observedGeneration,omitempty"`
	depv1a1.DependencyManager `json:",inline"`
	Conditions                []metav1.Condition `json:"conditions,omitempty"`
}

type ZoneInstanceTypeSpec struct {
	ZoneName string             `json:"zone" yaml:"zone"`
	Flavors  []InstanceOffering `json:"flavors" yaml:"flavors"`
}

type InstanceOffering struct {
	Name        string   `json:"name" yaml:"name"`
	NameLabel   string   `json:"nameLabel" yaml:"nameLabel"`
	VCPUs       int      `json:"vcpus" yaml:"vcpus"`
	RAM         string   `json:"ram" yaml:"ram"`
	Price       string   `json:"price,omitempty" yaml:"price,omitempty"`
	GPU         GPU      `json:"gpu,omitempty" yaml:"gpu,omitempty"`
	Generation  string   `json:"generation,omitempty" yaml:"generation,omitempty"`
	VolumeTypes []string `json:"volumeTypes,omitempty" yaml:"volumeTypes,omitempty"`
	Spot        Spot     `json:"spot,omitempty" yaml:"spot,omitempty"`
}

type GPU struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	Manufacturer string `json:"manufacturer" yaml:"manufacturer"`
	Count        int    `json:"count" yaml:"count"`
	Model        string `json:"model" yaml:"model"`
	Memory       string `json:"memory" yaml:"memory"`
}

type Spot struct {
	Price   string `json:"price" yaml:"price"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".status.region"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
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

// helper to set a condition
func (s *InstanceTypeStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
