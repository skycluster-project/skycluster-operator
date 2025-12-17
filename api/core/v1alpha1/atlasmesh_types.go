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

// AtlasMeshSpec defines the desired state of AtlasMesh
type AtlasMeshSpec struct {
	Approve             bool                `json:"approve"`
	DataflowPolicyRef   DataflowPolicyRef   `json:"dataflowPolicyRef,omitempty"`
	DeploymentPolicyRef DeploymentPolicyRef `json:"deploymentPlanRef,omitempty"`
	DeployMap           DeployMap           `json:"deployPlan,omitempty"`
}

// AtlasMeshStatus defines the observed state of AtlasMesh.
type AtlasMeshStatus struct {
	Objects    []hv1a1.SkyService `json:"objects,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Approved",type="boolean",JSONPath=".spec.approve",description="Indicates if the AtlasMesh is approved"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Indicates if the AtlasMesh is ready"

// AtlasMesh is the Schema for the atlasmeshs API
type AtlasMesh struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of AtlasMesh
	// +required
	Spec AtlasMeshSpec `json:"spec"`

	// status defines the observed state of AtlasMesh
	// +optional
	Status AtlasMeshStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Indicates if the AtlasMesh is ready"

// AtlasMeshList contains a list of AtlasMesh
type AtlasMeshList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AtlasMesh `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AtlasMesh{}, &AtlasMeshList{})
}

// helper to set a condition
func (s *AtlasMeshStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
