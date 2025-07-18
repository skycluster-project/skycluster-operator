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

type ProviderSpec struct {
	// +kubebuilder:validation:Enum=aws;azure;gpc;openstack
	Platform    string      `json:"platform"`
	Region      string      `json:"region"`
	RegionAlias string      `json:"regionAlias"`
	Continent   string      `json:"continent,omitempty"`
	Gateway     GatewaySpec `json:"gateway,omitempty"`
	Enabled     bool        `json:"enabled"`
	Zones       []ZoneSpec  `json:"zones"`
}

type GatewaySpec struct {
	Ip string `json:"ip"`
}

type ZoneSpec struct {
	Name         string `json:"name"`
	LocationName string `json:"locationName,omitempty"`
	DefaultZone  bool   `json:"defaultZone"`
	Enabled      bool   `json:"enabled"`
	BorderGroup  string `json:"borderGroup,omitempty"`
	Type         string `json:"type"`
}

type ProviderStatus struct {
	Enabled bool       `json:"enabled,omitempty"`
	Zones   []ZoneSpec `json:"zones,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Provider is the Schema for the providers API
type Provider struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Provider
	// +required
	Spec ProviderSpec `json:"spec"`

	// status defines the observed state of Provider
	// +optional
	Status ProviderStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Provider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
}
