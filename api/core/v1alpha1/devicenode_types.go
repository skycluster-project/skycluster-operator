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
)

// DeviceNodeSpec defines the desired state of DeviceNode
type DeviceNodeSpec struct {
	ProviderRef string `json:"providerRef"`
	DeviceSpec  *DeviceZoneSpec `json:"deviceSpec"`
}

type DeviceZoneSpec struct {
	Type      string             `json:"type" yaml:"type"`
	Zone      string             `json:"zone" yaml:"zone"`
	PrivateIp string             `json:"privateIp" yaml:"privateIp"`
	PublicIp  string             `json:"publicIp,omitempty" yaml:"publicIp,omitempty"`
	Auth      *DeviceNodeAuth    `json:"auth" yaml:"auth"`
	Configs   *hv1a1.InstanceOffering   `json:"configs,omitempty" yaml:"configs,omitempty"`
}

type DeviceNodeAuth struct {
	Username            string               `json:"username" yaml:"username"`
	PrivateKeySecretRef *SecretKeySelector 	`json:"privateKeySecretRef,omitempty" yaml:"privateKeySecretRef,omitempty"`
}

type SecretKeySelector struct {
	// Name of the secret
	Name string `json:"name" yaml:"name"`
	// Key in the secret
	Key string `json:"key" yaml:"key"`
}

// DeviceNodeStatus defines the observed state of DeviceNode.
type DeviceNodeStatus struct {
	Region             string         `json:"region,omitempty"`
	Zone               string         `json:"zone,omitempty"`
	LastUpdateTime     metav1.Time     `json:"lastUpdateTime,omitempty"`
	ObservedGeneration int64           `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".status.region"
// +kubebuilder:printcolumn:name="Zone",type="string",JSONPath=".status.zone"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

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


func (s *DeviceNodeStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
