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

type DataDapendency struct {
	// From is the reference to the source component
	From corev1.ObjectReference `json:"from,omitempty"`
	// To is the reference to the destination component
	To corev1.ObjectReference `json:"to,omitempty"`

	// Latency represents the latency between services
	Latency string `json:"latency,omitempty"`

	// TotalDataTransfer indicates the total data transferred
	TotalDataTransfer string `json:"totalDataTransfer,omitempty"`

	// AverageDataRate indicates the average data rate, [not supported yet]
	AverageDataRate string `json:"averageDataRate,omitempty"`
}

// DataflowPolicySpec defines the desired state of DataflowPolicy.
type DataflowPolicySpec struct {
	// DataDependencies is the list of data dependencies
	DataDependencies []DataDapendency `json:"dataDependencies"`
}

// DataflowPolicyStatus defines the observed state of DataflowPolicy.
type DataflowPolicyStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DataflowPolicy is the Schema for the dataflowpolicies API.
type DataflowPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataflowPolicySpec   `json:"spec,omitempty"`
	Status DataflowPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DataflowPolicyList contains a list of DataflowPolicy.
type DataflowPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataflowPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataflowPolicy{}, &DataflowPolicyList{})
}

func (in *DataflowPolicy) SetCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
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

func (in *DataflowPolicy) addCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
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
