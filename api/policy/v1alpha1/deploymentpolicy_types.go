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

type CustomMetric struct {
	// Name is the name of the custom metric
	Name string `json:"name"`
	// Endpoint is the endpoint of the custom metric
	Endpoint string `json:"endpoint"`
}

type PerformanceConstraint struct {
	ResponseTime  string         `json:"responseTime,omitempty"`
	CustomMetrics []CustomMetric `json:"customMetrics,omitempty"`
}

type DeploymentPolicyItem struct {
	// ComponentRef is the reference to the component
	ComponentRef corev1.ObjectReference `json:"componentRef"`
	VirtualServiceConstraint []VirtualServiceConstraint `json:"virtualServiceConstraint,omitempty"`
	// PerformanceConstraint is the performance constraint for the component
	PerformanceConstraint PerformanceConstraint `json:"performanceConstraint,omitempty"`
	// LocationConstraint is the location constraint for the component
	LocationConstraint hv1a1.LocationConstraint `json:"locationConstraint,omitempty"`
}

type VirtualServiceConstraint struct {
	AnyOf []VirtualServiceSelector `json:"anyOf"`
}

type VirtualServiceSelector struct {
	hv1a1.VirtualService `json:",inline"`
	Count          int   `json:"count,omitempty"`
}

// DeploymentPolicySpec defines the desired state of DeploymentPolicy.
type DeploymentPolicySpec struct {
	DeploymentPolicies []DeploymentPolicyItem `json:"deploymentPolicies"`
}

// DeploymentPolicyStatus defines the observed state of DeploymentPolicy.
type DeploymentPolicyStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status",description="The sync status of the DeploymentPolicy"

// DeploymentPolicy is the Schema for the deploymentpolicies API.
type DeploymentPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeploymentPolicySpec   `json:"spec,omitempty"`
	Status DeploymentPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DeploymentPolicyList contains a list of DeploymentPolicy.
type DeploymentPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeploymentPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DeploymentPolicy{}, &DeploymentPolicyList{})
}


func (s *DeploymentPolicyStatus) SetCondition(condition hv1a1.Condition, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    string(condition),
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
