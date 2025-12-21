package core

import (
	appsv1 "k8s.io/api/apps/v1"
)

// deplyHasLabels returns true if the deployment has the given labels
func deploymentHasLabels(deploy *appsv1.Deployment, labels map[string]string) bool {
	for k, v := range labels {
		if deploy.Spec.Template.ObjectMeta.Labels[k] != v {
			return false
		}
	}
	return true
}
