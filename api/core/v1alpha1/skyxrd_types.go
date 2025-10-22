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

// SkyXRDSpec defines the desired state of SkyXRD.
type SkyXRDSpec struct {
	// Manifests is a list of manifests to apply to the cluster
	Approve bool `json:"approve"`
	DataflowPolicyRef  DataflowPolicyRef `json:"dataflowPolicyRef,omitempty"`
	DeploymentPolicyRef  DeploymentPolicyRef `json:"deploymentPlanRef,omitempty"`
	DeployMap hv1a1.DeployMap `json:"deployPlan,omitempty"`
}

// SkyXRDStatus defines the observed state of SkyXRD.
type SkyXRDStatus struct {
	Manifests  []hv1a1.SkyService `json:"manifests,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Approved",type="boolean",JSONPath=".spec.approve",description="Indicates if the SkyXRD is approved"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Indicates if the SkyXRD is ready"

// SkyXRD is the Schema for the skyxrds API.
type SkyXRD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkyXRDSpec   `json:"spec,omitempty"`
	Status SkyXRDStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Indicates if the SkyXRD is ready"

// SkyXRDList contains a list of SkyXRD.
type SkyXRDList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyXRD `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyXRD{}, &SkyXRDList{})
}

// helper to set a condition
func (s *SkyXRDStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
