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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SkyVMSpec defines the desired state of SkyVM.
type SkyVMSpec struct {
	MachineType          string                 `json:"machineType,omitempty"`
	DiskSizeGB           int                    `json:"diskSizeGB,omitempty"`
	DiskType             string                 `json:"diskType,omitempty"`
	Preemptible          bool                   `json:"preemptible,omitempty"`
	Image                string                 `json:"image,omitempty"`
	MonitoringMetricList []MonitoringMetricSpec `json:"monitoringMetrics,omitempty"`
	ProviderRef          ProviderRefSpec        `json:"providerRef,omitempty"`
	SkyDependencyList    []SkyDependency        `json:"skyDependencies,omitempty"`
}

// SkyVMStatus defines the observed state of SkyVM.
type SkyVMStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SkyVM is the Schema for the skyvms API.
type SkyVM struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkyVMSpec   `json:"spec,omitempty"`
	Status SkyVMStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SkyVMList contains a list of SkyVM.
type SkyVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyVM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyVM{}, &SkyVMList{})
}

func (obj *SkyVM) GetProviderRef() ProviderRefSpec {
	return obj.Spec.ProviderRef
}
