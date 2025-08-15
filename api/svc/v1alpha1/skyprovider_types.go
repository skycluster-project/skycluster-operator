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
	h "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OverlaySpec struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

type ProviderGatewaySpec struct {
	Flavor    string `json:"flavor,omitempty"`
	PublicKey string `json:"publicKey,omitempty"`
	PrivateIP string `json:"privateIP,omitempty"`
	PublicIP  string `json:"publicIP,omitempty"`
	// Overlay is the overlay server configuration
	Overlay OverlaySpec `json:"overlay,omitempty"`
	// VpcCidr is the main CIDR block for the provider and its gateway
	// /16 CIDR block will be used for the provider and /24 CIDR block
	// will be used for the gateway network.
	//	// +kubebuilder:validation:Pattern="^([0-9]{1,3}\.){3}0/24$"
	VpcCidr string `json:"vpcCidr"`
}

// SkyProviderSpec defines the desired state of SkyProvider.
type SkyProviderSpec struct {
	ProviderGateway ProviderGatewaySpec          `json:"providerGateway"`
	ProviderRef     h.ProviderRefSpec `json:"providerRef"`
	Monitoring      h.MonitoringSpec               `json:"monitoring,omitempty"`
}

// SkyProviderStatus defines the observed state of SkyProvider.
type SkyProviderStatus struct {
	Conditions      []metav1.Condition  `json:"conditions,omitempty"`
	ProviderGateway ProviderGatewaySpec `json:"providerGateway,omitempty"`
	Retries         int                 `json:"retries,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SkyProvider is the Schema for the skyproviders API.
type SkyProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkyProviderSpec   `json:"spec,omitempty"`
	Status SkyProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SkyProviderList contains a list of SkyProvider.
type SkyProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyProvider{}, &SkyProviderList{})
}
