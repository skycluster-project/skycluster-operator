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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SkyClusterSpec defines the desired state of SkyCluster.
type SkyClusterSpec struct {
	// DataflowPolicyRef is the reference to the DataflowPolicy
	// The DataflowPolicy is used to define the dataflow policy and
	// Should has the same name and namespace as the SkyCluster
	DataflowPolicyRef corev1.LocalObjectReference `json:"dataflowPolicyRef,omitempty"`
	// DeploymentPolicyRef is the reference to the DeploymentPolicy
	// The DeploymentPolicy is used to define the deployment policy and
	// Should has the same name and namespace as the SkyCluster
	DeploymentPolciyRef corev1.LocalObjectReference `json:"deploymentPolicyRef,omitempty"`
	// SkyProviders is the list of the SkyProvider that will be provisioned
	// This list should be populated when the optimizer is executed
	SkyProviders []ProviderRefSpec `json:"skyProviders,omitempty"`
	// SkyComponents is the list of the SkyComponent including deployments
	// and all Sky Services such as SkyVM that will be used by the optimization.
	SkyComponents []SkyComponent `json:"skyComponents,omitempty"`
}

// SkyClusterStatus defines the observed state of SkyCluster.
type SkyClusterStatus struct {
	// Optimization is the status of the optimization
	Optimization OptimizationSpec   `json:"optimization,omitempty"`
	Conditions   []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SkyCluster is the Schema for the skyclusters API.
type SkyCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkyClusterSpec   `json:"spec,omitempty"`
	Status SkyClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SkyClusterList contains a list of SkyCluster.
type SkyClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyCluster{}, &SkyClusterList{})
}

func (in *SkyCluster) SetCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
	var c *metav1.Condition
	for i := range in.Status.Conditions {
		if in.Status.Conditions[i].Type == ctype {
			c = &in.Status.Conditions[i]
		}
	}
	if c == nil {
		in.addCondition(ctype, status, reason, message)
	} else {
		// check message ?
		if c.Status == status && c.Reason == reason && c.Message == message {
			return
		}
		now := metav1.Now()
		if c.Status != status {
			c.LastTransitionTime = now
		}
		c.Status = status
		c.Reason = reason
		c.Message = message
	}
}

func (in *SkyCluster) addCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	c := metav1.Condition{
		Type:               ctype,
		LastTransitionTime: now,
		Status:             status,
		Reason:             reason,
		Message:            message,
	}
	in.Status.Conditions = append(in.Status.Conditions, c)
}
