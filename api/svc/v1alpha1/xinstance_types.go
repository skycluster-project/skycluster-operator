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
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// XInstanceSpec defines the desired state of XInstance
// XInstanceSpec defines the desired state of XInstance
type XInstanceSpec struct {
	// applicationId is a unique identifier for the setup/application.
	// Must match the applicationId used in the provider instance (for AWS).
	// +kubebuilder:validation:MinLength=1
	ApplicationID string `json:"applicationId"`

	// Flavor of the instance, defined like "2vCPU-4GB-1xA100-32GB".
	Flavor hv1a1.ComputeFlavor `json:"flavor"`

	// PreferSpot indicates whether spot instances should be used when available.
	// +optional
	PreferSpot bool `json:"preferSpot,omitempty"`

	// Image references an Images custom resource (images.core.skycluster.io), e.g. "ubuntu-22.04".
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// PublicKey is the SSH public key to be installed. If omitted, a default public key will be used.
	// +optional
	PublicKey string `json:"publicKey,omitempty"`

	// PublicIP controls whether the instance receives a public IP address.
	// +optional
	PublicIP bool `json:"publicIp,omitempty"`

	// UserData is optional cloud-init (or other) user data.
	// +optional
	UserData string `json:"userData,omitempty"`

	// Security groups: lists of allowed TCP/UDP port ranges.
	// +optional
	SecurityGroups *SecurityGroups `json:"securityGroups,omitempty"`

	// RootVolumes describes root volumes to attach to the instance.
	// +optional
	RootVolumes []RootVolume `json:"rootVolumes,omitempty"`

	// ProviderRef must reference the provider configuration used for provisioning.
	ProviderRef hv1a1.ProviderRefSpec `json:"providerRef"`
}

// SecurityGroups defines allowed TCP and UDP port ranges.
type SecurityGroups struct {
	// TCP ports to open.
	// +optional
	TCPPorts []PortRange `json:"tcpPorts,omitempty"`

	// UDP ports to open.
	// +optional
	UDPPorts []PortRange `json:"udpPorts,omitempty"`
}

// PortRange defines a port range and protocol.
type PortRange struct {
	// FromPort is the starting port in the range.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	FromPort int32 `json:"fromPort"`

	// ToPort is the ending port in the range.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	ToPort int32 `json:"toPort"`

	// Protocol for the range (tcp or udp).
	// +kubebuilder:validation:Enum=tcp;udp
	Protocol string `json:"protocol"`
}

// RootVolume describes a root volume for the instance.
type RootVolume struct {
	// Size of the volume in GB (string here to match provided manifest style).
	// e.g. "20"
	// +kubebuilder:validation:Pattern=`^[0-9]+$`
	Size string `json:"size"`

	// Type of the volume, e.g. "gp2" for AWS or "pd-standard" for GCP.
	// +optional
	Type string `json:"type"`
}

// XInstanceStatus defines the observed state of XInstance.
type XInstanceStatus struct {
	// // SelectedComputeProfile lists the compute profiles selected for this XInstance.
	// // +optional
	// SelectedComputeProfile []hv1a1.VirtualService `json:"selectedComputeProfile,omitempty"`
	// // AttemptComputeProfile lists the compute profiles attempted for this XInstance.
	// // +optional
	// AttemptedComputeProfile []hv1a1.VirtualService `json:"attemptedComputeProfile,omitempty"`
	// The status of each condition is one of True, False, or Unknown.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// XInstance is the Schema for the xinstances API
type XInstance struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of XInstance
	// +required
	Spec XInstanceSpec `json:"spec"`

	// status defines the observed state of XInstance
	// +optional
	Status XInstanceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// XInstanceList contains a list of XInstance
type XInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []XInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&XInstance{}, &XInstanceList{})
}
