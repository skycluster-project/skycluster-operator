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

// XKubeSpec defines the desired state of XKube.
// This spec supports manifests for both AWS and GCP provider variants.
type XKubeSpec struct {
	// applicationId is a unique identifier for the setup/application.
	// Must match the one used by the provider.
	// +kubebuilder:validation:MinLength=1
	ApplicationID string `json:"applicationId"`

	// serviceCidr is the CIDR for services (non-overlapping with VPC/node CIDRs).
	// Optional (commonly used in AWS manifests).
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}\/[0-9]{1,2}$`
	ServiceCidr string `json:"serviceCidr,omitempty"`

	// nodeCidr is an optional node CIDR (commonly used in GCP manifests).
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}\/[0-9]{1,2}$`
	NodeCidr string `json:"nodeCidr,omitempty"`

	// podCidr describes pod CIDRs. For AWS this may include separate public/private sub-blocks.
	// For GCP it may only include cidr.
	// +optional
	PodCidr *PodCidr `json:"podCidr,omitempty"`

	// nodeGroups defines one or more node groups for the cluster.
	// +optional
	// +listType=set
	NodeGroups []NodeGroup `json:"nodeGroups,omitempty"`

	// principal identifies the principal used to perform control-plane actions.
	// +optional
	Principal *Principal `json:"principal,omitempty"`

	// Provider reference must match the provider instance configuration.
	ProviderRef hv1a1.ProviderRefSpec `json:"providerRef"`
}

// PodCidr represents pod networking blocks. Public/private are optional subdivisions.
type PodCidr struct {
	// CIDR block for pods.
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}\/[0-9]{1,2}$`
	Cidr string `json:"cidr"`

	// Optional: public pods CIDR (AWS example)
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}\/[0-9]{1,2}$`
	Public string `json:"public,omitempty"`

	// Optional: private pods CIDR (AWS example)
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}\/[0-9]{1,2}$`
	Private string `json:"private,omitempty"`
}

// NodeGroup defines a group of worker nodes and optional autoscaling.
type NodeGroup struct {
	// InstanceTypes lists instance flavor identifiers, e.g. ["4vCPU-16GB"].
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	InstanceTypes []string `json:"instanceTypes"`

	// publicAccess indicates whether nodes in this group receive public access.
	PublicAccess bool `json:"publicAccess"`

	// AutoScaling settings (optional).
	// +optional
	AutoScaling *AutoScaling `json:"autoScaling,omitempty"`
}

// AutoScaling contains optional autoscaler settings for a node group.
type AutoScaling struct {
	// Minimum number of nodes.
	// +kubebuilder:validation:Minimum=0
	MinSize int32 `json:"minSize"`

	// Maximum number of nodes.
	// +kubebuilder:validation:Minimum=1
	MaxSize int32 `json:"maxSize"`
}

// Principal identifies who/what performs control plane actions.
type Principal struct {
	// Type of principal: user | role | serviceAccount | servicePrincipal | managedIdentity
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type"`

	// ID depends on platform: ARN for AWS, member for GCP, principalId for Azure, etc.
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`
}

// XKubeStatus defines the observed state of XKube.
type XKubeStatus struct {
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// XKube is the Schema for the xkubes API
type XKube struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of XKube
	// +required
	Spec XKubeSpec `json:"spec"`

	// status defines the observed state of XKube
	// +optional
	Status XKubeStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// XKubeList contains a list of XKube
type XKubeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []XKube `json:"items"`
}

func init() {
	SchemeBuilder.Register(&XKube{}, &XKubeList{})
}
