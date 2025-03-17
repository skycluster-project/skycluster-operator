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
	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SkyAppSpec defines the desired state of SkyApp.
type SkyAppSpec struct {
	// Manifests is a list of manifests that should be applied to the cluster
	Manifests []corev1alpha1.SkyService `json:"manifests,omitempty"`
}

// SkyAppStatus defines the observed state of SkyApp.
type SkyAppStatus struct {
	// Objects is a list of objects that will be created in the remote cluster
	// Based on the manifests in the spec
	Objects []corev1alpha1.SkyService `json:"objects,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SkyApp is the Schema for the skyapps API.
type SkyApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkyAppSpec   `json:"spec,omitempty"`
	Status SkyAppStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SkyAppList contains a list of SkyApp.
type SkyAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SkyApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SkyApp{}, &SkyAppList{})
}
