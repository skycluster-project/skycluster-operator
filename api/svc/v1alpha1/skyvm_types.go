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
	// xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SecGroup struct {
	// Description is the description of the security group
	Description string `json:"description,omitempty"`
	// TCPPorts is the list of TCP ports to open
	TCPPorts []PortSpec `json:"tcpPorts,omitempty"`
	// UDPPorts is the list of UDP ports to open
	UDPPorts []PortSpec `json:"udpPorts,omitempty"`
}

type PortSpec struct {
	// FromPort is the starting port number
	FromPort int `json:"fromPort"`
	// ToPort is the ending port number
	ToPort int `json:"toPort"`
}

// SkyVMSpec defines the desired state of SkyVM.
type SkyVMSpec struct {
	// Flavor is the size of the VM
	Flavor string `json:"flavor,omitempty"`
	// Image is the image to use for the VM
	Image string `json:"image,omitempty"`
	// UserData is the cloud-init script to run on the VM, shoud follow the cloud-init format
	UserData string `json:"userData,omitempty"`
	// PublicIP is whether the VM should have a public IP
	PublicIP bool `json:"publicIp,omitempty"`
	// PublicKey is the SSH public key to add to the VM
	PublicKey string `json:"publicKey,omitempty"`
	// IPForwarding is whether the VM should have IP forwarding enabled
	IPForwarding bool `json:"iPForwarding,omitempty"`
	// SecGroup is the security group definition to apply to the VM
	SecGroup []SecGroup `json:"secGroup,omitempty"`
	// ProviderRef is the reference to the provider that this VM should be deployed to
	ProviderRef corev1alpha1.ProviderRefSpec `json:"providerRef,omitempty"`
}

// SkyVMStatus defines the observed state of SkyVM.
type SkyVMStatus struct {
	DependsOn  []corev1.ObjectReference `json:"dependsOn,omitempty"`
	DependedBy []corev1.ObjectReference `json:"dependedBy,omitempty"`
}

// Other fields: Conditions xpv1.Condition           `json:"conditions,omitempty"`

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SkyVM is the Schema for the skyvms API.
type SkyVM struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkyVMSpec   `json:"spec,omitempty"`
	Status SkyVMStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SkyVMList contains a list of SkyVM.
type SkyVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyVM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyVM{}, &SkyVMList{})
}
