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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var dpLogger = logf.Log.WithName("deploymentpolicy-resource")

// SetupDeploymentPolicyWebhookWithManager registers the webhook for DeploymentPolicy in the manager.
func SetupDeploymentPolicyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&pv1a1.DeploymentPolicy{}).
		WithValidator(&DeploymentPolicyCustomValidator{}).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-policy-skycluster-io-v1alpha1-deploymentpolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=policy.skycluster.io,resources=deploymentpolicies,verbs=create;update,versions=v1alpha1,name=vdeploymentpolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// DeploymentPolicyCustomValidator struct is responsible for validating the DeploymentPolicy resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type DeploymentPolicyCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &DeploymentPolicyCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type DeploymentPolicy.
func (v *DeploymentPolicyCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dp, ok := obj.(*pv1a1.DeploymentPolicy)
	if !ok {
		return nil, fmt.Errorf("expected a DeploymentPolicy object but got %T", obj)
	}
	
	labels := []string{"skycluster.io/app-id", "skycluster.io/app-scope"}
	if dp.Labels == nil {
		return nil, fmt.Errorf("missing required labels on DeploymentPolicy '%s': %v", dp.GetName(), labels)
	} else {
		for _, label := range labels {
			if _, exists := dp.Labels[label]; !exists {
				return nil, fmt.Errorf("missing required label '%s' on DeploymentPolicy '%s'", label, dp.GetName())
			}
		}
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type DeploymentPolicy.
func (v *DeploymentPolicyCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dp, ok := newObj.(*pv1a1.DeploymentPolicy)
	if !ok {
		return nil, fmt.Errorf("expected a DeploymentPolicy object for the newObj but got %T", newObj)
	}
	
	labels := []string{"skycluster.io/app-id", "skycluster.io/app-scope"}
	if dp.Labels == nil {
		return nil, fmt.Errorf("missing required labels on DeploymentPolicy '%s': %v", dp.GetName(), labels)
	} else {
		for _, label := range labels {
			if _, exists := dp.Labels[label]; !exists {
				return nil, fmt.Errorf("missing required label '%s' on DeploymentPolicy '%s'", label, dp.GetName())
			}
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type DeploymentPolicy.
func (v *DeploymentPolicyCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	_, ok := obj.(*pv1a1.DeploymentPolicy)
	if !ok {
		return nil, fmt.Errorf("expected a DeploymentPolicy object but got %T", obj)
	}

	return nil, nil
}
