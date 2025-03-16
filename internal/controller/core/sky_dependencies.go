package core

import (
	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Dependencies List (namespaced)
// Namespace and other required fields should be set within the
// respective controllers

type SkyDependency struct {
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Group      string `json:"group"`
	Version    string `json:"version,omitempty"`
	Replicas   int    `json:"replicas,omitempty"`
}

type SkyVMDependencyMap struct {
	Updated         bool                       `json:"updated,omitempty"`
	Created         bool                       `json:"created,omitempty"`
	Deleted         bool                       `json:"deleted,omitempty"`
	UnstructuredObj *unstructured.Unstructured `json:"unstructuredObj,omitempty"`
}

var SkyDependencies = map[string][]SkyDependency{
	"SkyProvider": []SkyDependency{
		SkyDependency{
			Kind:       "SkyProvider",
			APIVersion: corev1alpha1.SKYCLUSTER_XRDsGROUP + "/" + corev1alpha1.SKYCLUSTER_VERSION,
			Replicas:   1,

			// Deprecated
			Group: corev1alpha1.SKYCLUSTER_XRDsGROUP,
			// Deprecated
			Version: corev1alpha1.SKYCLUSTER_VERSION,
		},
	},
	"SkyVM": []SkyDependency{
		SkyDependency{
			Kind:       "SkyProvider",
			APIVersion: corev1alpha1.SKYCLUSTER_XRDsGROUP + "/" + corev1alpha1.SKYCLUSTER_VERSION,
			Replicas:   1,

			// Deprecated
			Group:   corev1alpha1.SKYCLUSTER_XRDsGROUP,
			Version: corev1alpha1.SKYCLUSTER_VERSION,
		},
	},
}
