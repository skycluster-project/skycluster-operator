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

type OptimizationSpec struct {
	// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Unknown
	Status       string                      `json:"status,omitempty"`
	Result       string                      `json:"result,omitempty"`
	DeployMap    DeployMap                   `json:"deployMap,omitempty"`
	ConfigMapRef corev1.LocalObjectReference `json:"configMapRef,omitempty"`
	PodRef       corev1.LocalObjectReference `json:"podRef,omitempty"`
}

// ILPTaskSpec defines the desired state of ILPTask.
type ILPTaskSpec struct {
	SkyComponents []SkyComponent `json:"skyComponents"`
}

// ILPTaskStatus defines the observed state of ILPTask.
type ILPTaskStatus struct {
	Optimization OptimizationSpec   `json:"optimization,omitempty"`
	Conditions   []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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

func (in *ILPTask) SetCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
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

func (in *ILPTask) addCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
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
