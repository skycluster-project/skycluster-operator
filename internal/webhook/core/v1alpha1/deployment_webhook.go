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
	"context"

	cv1a1 "github.com/skycluster-project/skycluster-operator/internal/controller"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// nolint:unused

// SetupDeploymentWebhookWithManager registers the webhook for Deployment in the manager.
func SetupDeploymentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&appsv1.Deployment{}).
		WithDefaulter(&DeploymentCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-apps-v1-deployment,mutating=true,failurePolicy=fail,sideEffects=None,groups="apps",resources=deployments,verbs=create;update,versions=v1,name=mdeployment.kb.io,admissionReviewVersions=v1

// DeploymentCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Deployment when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type DeploymentCustomDefaulter struct {
}

var _ webhook.CustomDefaulter = &DeploymentCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Deployment.
func (d *DeploymentCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	logger := log.FromContext(ctx)
	logger.Info("Deployment Custom Defaulter invoked")

	deployment, ok := obj.(*appsv1.Deployment)
	exists := cv1a1.HasAllLabelsAndValue(deployment.Labels, map[string]string{
		"skycluster.io/app-scope": "distributed",
	})
	if !ok || !exists {
		return nil
	} else {
		if !deployment.Spec.Paused {
			logger.Info("Webhook invoked. Setting paused to true.", "deployment", deployment.Name)
			deployment.Spec.Paused = true
		} else {
			logger.Info("Deployment already paused; skipping mutation.", "deployment", deployment.Name)
		}
	}
	return nil
}
