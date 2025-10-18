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

// LatencySpec defines the desired state of Latency
type LatencySpec struct {
	// ProviderA and ProviderB are canonical sorted provider names (A < B)
	ProviderRefA corev1.LocalObjectReference `json:"providerRefA"`
	ProviderRefB corev1.LocalObjectReference `json:"providerRefB"`
	// FixedLatencyMs is the seeded latency (from your fixed table)
	FixedLatencyMs string `json:"fixedLatencyMs,omitempty"`
}

// LatencyStatus defines the observed state of Latency.
type LatencyStatus struct {
	LastMeasuredMs  string           `json:"lastMeasuredMs,omitempty"`
	LastMeasuredAt  *metav1.Time   `json:"lastMeasuredAt,omitempty"`
	P50             string           `json:"p50,omitempty"`
	P95             string           `json:"p95,omitempty"`
	P99             string           `json:"p99,omitempty"`
	MeasurementCount int           `json:"measurementCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Last_Latency(ms)",type=string,JSONPath=".status.lastMeasuredMs"
// +kubebuilder:printcolumn:name="P99(ms)",type=string,JSONPath=".status.p99"
// +kubebuilder:printcolumn:name="Last_Measured",type=string,JSONPath=".status.lastMeasuredAt"

// Latency is the Schema for the latencies API
type Latency struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Latency
	// +required
	Spec LatencySpec `json:"spec"`

	// status defines the observed state of Latency
	// +optional
	Status LatencyStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// LatencyList contains a list of Latency
type LatencyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Latency `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Latency{}, &LatencyList{})
}

