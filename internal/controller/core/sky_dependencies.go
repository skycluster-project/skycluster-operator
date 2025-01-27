package core

import (
	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
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

var SkyDependencies = map[string][]SkyDependency{
	"SkyProvider": []SkyDependency{
		SkyDependency{
			Kind:     "SkyProvider",
			Group:    corev1alpha1.SkyClusterXRDsGroup,
			Version:  corev1alpha1.SkyClusterVersion,
			Replicas: 1,
		},
	},
}
