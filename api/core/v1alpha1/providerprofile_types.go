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

// ProviderProfileSpec defines the desired state of ProviderProfile
type ProviderProfileSpec struct {
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

// ProviderProfileStatus defines the observed state of ProviderProfile.
type ProviderProfileStatus struct {
	Enabled       bool       `json:"enabled,omitempty"`
	Region        string     `json:"region,omitempty"`
	Zones         []ZoneSpec `json:"zones,omitempty"`
	ConfigMapRef  string     `json:"configMapRef,omitempty"`
	Sync          bool       `json:"sync,omitempty"`
	TotalServices int        `json:"totalServices,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".status.region"
// +kubebuilder:printcolumn:name="Enabled",type="boolean",JSONPath=".status.enabled"
// +kubebuilder:printcolumn:name="Sync",type="boolean",JSONPath=".status.sync"
// +kubebuilder:printcolumn:name="Total Services",type="integer",JSONPath=".status.totalServices"

// ProviderProfile is the Schema for the providerprofiles API
type ProviderProfile struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ProviderProfile
	// +required
	Spec ProviderProfileSpec `json:"spec"`

	// status defines the observed state of ProviderProfile
	// +optional
	Status ProviderProfileStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ProviderProfileList contains a list of ProviderProfile
type ProviderProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProviderProfile{}, &ProviderProfileList{})
}
