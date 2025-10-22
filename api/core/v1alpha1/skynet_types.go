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

	k8sobj "github.com/crossplane-contrib/provider-kubernetes/apis/cluster/object/v1alpha2"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

// SkyNetSpec defines the desired state of SkyNet
type SkyNetSpec struct {
	Approve bool `json:"approve"`
	DataflowPolicyRef  DataflowPolicyRef `json:"dataflowPolicyRef,omitempty"`
	DeploymentPolicyRef  DeploymentPolicyRef `json:"deploymentPlanRef,omitempty"`
	DeployMap hv1a1.DeployMap `json:"deployPlan,omitempty"`
}

// SkyNetStatus defines the observed state of SkyNet.
type SkyNetStatus struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Manifests []k8sobj.Object `json:"manifests,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Approved",type="boolean",JSONPath=".spec.approve",description="Indicates if the SkyNet is approved"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Indicates if the SkyNet is ready"

// SkyNet is the Schema for the skynets API
type SkyNet struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of SkyNet
	// +required
	Spec SkyNetSpec `json:"spec"`

	// status defines the observed state of SkyNet
	// +optional
	Status SkyNetStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Indicates if the SkyNet is ready"

// SkyNetList contains a list of SkyNet
type SkyNetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyNet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyNet{}, &SkyNetList{})
}


// helper to set a condition
func (s *SkyNetStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
