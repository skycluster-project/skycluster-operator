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
	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GatewaySpec struct {
	Flavor    string `json:"flavor,omitempty"`
	PublicKey string `json:"publicKey,omitempty"`
	PrivateIP string `json:"privateIP,omitempty"`
	PublicIP  string `json:"publicIP,omitempty"`
}

type OverlaySpec struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

type SkySetupParameters struct {
	// Gateway is the gateway configuration for the provider
	Gateway GatewaySpec `json:"gateway,omitempty"`
	// Overlay is the overlay server configuration
	Overlay OverlaySpec `json:"overlay,omitempty"`
	// VpcCidr is the main CIDR block for the provider and its gateway
	// /16 CIDR block will be used for the provider and /24 CIDR block
	// will be used for the gateway network.
	//	// +kubebuilder:validation:Pattern="^([0-9]{1,3}\.){3}0/24$"
	VpcCidr string `json:"vpcCidr"`
}

// SkySetupSpec defines the desired state of SkySetup.
type SkySetupSpec struct {
	ForProvider SkySetupParameters `json:"forProvider"`
	// ProviderRef is the reference to the provider that this VM should be deployed to
	ProviderRef corev1alpha1.ProviderRefSpec `json:"providerRef"`
	// Monitoring is the monitoring configuration for the provider
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`
}

// SkySetupStatus defines the observed state of SkySetup.
type SkySetupStatus struct {
	// Conditions is an array of current conditions
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
	ForProvider SkySetupParameters `json:"forProvider,omitempty"`
	Retries     int                `json:"retries,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SkySetup is the Schema for the skysetups API.
type SkySetup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkySetupSpec   `json:"spec,omitempty"`
	Status SkySetupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SkySetupList contains a list of SkySetup.
type SkySetupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkySetup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkySetup{}, &SkySetupList{})
}
