package core

import (
	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Dependencies List (namespaced)
// Namespace and other required fields should be set within the
// respective controllers

type SkyDependency struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
	Group     string `json:"group"`
	Version   string `json:"version,omitempty"`
	Replicas  int    `json:"replicas,omitempty"`
}

type SkyVMDependencyMap struct {
	Updated         bool                       `json:"updated,omitempty"`
	Created         bool                       `json:"created,omitempty"`
	Deleted         bool                       `json:"deleted,omitempty"`
	SkyProviderObj  *corev1alpha1.SkyProvider  `json:"skyProviderObj,omitempty"`
	UnstructuredObj *unstructured.Unstructured `json:"unstructuredObj,omitempty"`
}

var SkyDependencies = map[string][]SkyDependency{
	"SkyProvider": []SkyDependency{
		SkyDependency{
			Kind:     "SkyProvider",
			Group:    corev1alpha1.SkyClusterXRDsGroup,
			Version:  corev1alpha1.SkyClusterVersion,
			Replicas: 1,
		},
	},
	"SkyVM": []SkyDependency{
		SkyDependency{
			Kind:     "SkyProvider",
			Group:    corev1alpha1.SkyClusterXRDsGroup,
			Version:  corev1alpha1.SkyClusterVersion,
			Replicas: 1,
		},
	},
}
