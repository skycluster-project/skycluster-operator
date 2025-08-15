package core

import (
	ha1va "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
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
			APIVersion: ha1va.SKYCLUSTER_XRDsGROUP + "/" + ha1va.SKYCLUSTER_VERSION,
			Replicas:   1,

			// Deprecated
			Group: ha1va.SKYCLUSTER_XRDsGROUP,
			// Deprecated
			Version: ha1va.SKYCLUSTER_VERSION,
		},
	},
	"SkyVM": []SkyDependency{
		SkyDependency{
			Kind:       "SkyProvider",
			APIVersion: ha1va.SKYCLUSTER_XRDsGROUP + "/" + ha1va.SKYCLUSTER_VERSION,
			Replicas:   1,

			// Deprecated
			Group:   ha1va.SKYCLUSTER_XRDsGROUP,
			Version: ha1va.SKYCLUSTER_VERSION,
		},
	},
}
