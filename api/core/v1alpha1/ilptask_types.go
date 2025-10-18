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
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

type OptimizationSpec struct {
	// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Unknown
	Status       string                      `json:"status,omitempty"`
	Result       string                      `json:"result,omitempty"`
	DeployMap    hv1a1.DeployMap                 `json:"deployMap,omitempty"`
	ConfigMapRef corev1.LocalObjectReference `json:"configMapRef,omitempty"`
	PodRef       corev1.LocalObjectReference `json:"podRef,omitempty"`
	DataflowResourceVersion        string    `json:"dataflowResourceVersion,omitempty"`
	DeploymentPlanResourceVersion  string    `json:"deploymentPlanResourceVersion,omitempty"`
}

// ILPTaskSpec defines the desired state of ILPTask.
type ILPTaskSpec struct {
	DataflowPolicyRef  corev1.LocalObjectReference `json:"dataflowPolicyRef,omitempty"`
	DeploymentPlanRef  corev1.LocalObjectReference `json:"deploymentPlanRef,omitempty"`
}

// ILPTaskStatus defines the observed state of ILPTask.
type ILPTaskStatus struct {
	Optimization OptimizationSpec   `json:"optimization,omitempty"`
	Conditions   []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.optimization.status",description="The status of the ILP Task"
// +kubebuilder:printcolumn:name="Result",type="string",JSONPath=".status.optimization.result",description="The result of the ILP Task"

// ILPTask is the Schema for the ilptasks API.
type ILPTask struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ILPTaskSpec   `json:"spec,omitempty"`
	Status ILPTaskStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ILPTaskList contains a list of ILPTask.
type ILPTaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ILPTask `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ILPTask{}, &ILPTaskList{})
}

func (s *ILPTaskStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
