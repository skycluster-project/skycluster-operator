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

	h "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

// type Location struct {
// 	// Name is the name of the location e.g. aws, gcp, os (OpenStack)
// 	Name string `json:"name,omitempty"`
// 	// Type is the type of the location e.g. cloud, nte, edge
// 	Type string `json:"type,omitempty"`
// 	// Region is the region of the location
// 	Region string `json:"region,omitempty"`
// 	// Zone is the zone of the location
// 	Zone string `json:"zone,omitempty"`
// }

// type LocationSet struct {
// 	AllOf []B1 `json:"allOf,omitempty"`
// }
// type B1 struct {
// 	AnyOf    []Location `json:"anyOf,omitempty"`
// 	Location Location   `json:"providerRef,omitempty"`
// }

type LocationConstraint struct {
	// Permitted is the list of locations that are permitted
	Permitted h.LocationPermittedRuleSet `json:"permitted,omitempty"`
	// Required is the list of locations that are required for deployment
	Required h.LocationRequiredRuleSet `json:"required,omitempty"`
}

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
	// PerformanceConstraint is the performance constraint for the component
	PerformanceConstraint PerformanceConstraint `json:"performanceConstraint,omitempty"`
	// LocationConstraint is the location constraint for the component
	LocationConstraint LocationConstraint `json:"locationConstraint,omitempty"`
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

func (in *DeploymentPolicy) SetCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
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

func (in *DeploymentPolicy) addCondition(ctype string, status metav1.ConditionStatus, reason, message string) {
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
