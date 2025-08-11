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

type EgressCostSpec struct {
	ProviderRef string `json:"providerRef"`
	ToInternet  string `json:"toInternet"`
	ToRegion    string `json:"toRegion"`
	ToZone      string `json:"toZone"`
}

// EgressCostStatus defines the observed state of EgressCost.
type EgressCostStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// EgressCost is the Schema for the egresscosts API
type EgressCost struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of EgressCost
	// +required
	Spec EgressCostSpec `json:"spec"`

	// status defines the observed state of EgressCost
	// +optional
	Status EgressCostStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// EgressCostList contains a list of EgressCost
type EgressCostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EgressCost `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EgressCost{}, &EgressCostList{})
}
