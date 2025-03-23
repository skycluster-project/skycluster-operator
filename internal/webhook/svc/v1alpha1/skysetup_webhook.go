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

	svcv1alpha1 "github.com/etesami/skycluster-manager/api/svc/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var skysetuplog = logf.Log.WithName("skysetup-resource")

// SetupSkySetupWebhookWithManager registers the webhook for SkySetup in the manager.
func SetupSkySetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&svcv1alpha1.SkySetup{}).
		WithValidator(&SkySetupCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-svc-skycluster-io-v1alpha1-skysetup,mutating=false,failurePolicy=fail,sideEffects=None,groups=svc.skycluster.io,resources=skysetups,verbs=create;update,versions=v1alpha1,name=vskysetup-v1alpha1.kb.io,admissionReviewVersions=v1

// SkySetupCustomValidator struct is responsible for validating the SkySetup resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type SkySetupCustomValidator struct{}

var _ webhook.CustomValidator = &SkySetupCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type SkySetup.
func (v *SkySetupCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	skysetup, ok := obj.(*svcv1alpha1.SkySetup)
	if !ok {
		return nil, fmt.Errorf("expected a SkySetup object but got %T", obj)
	}
	skysetuplog.Info("Validation for SkySetup upon creation", "name", skysetup.GetName())

	if skysetup.Spec.ForProvider.VpcCidr == "" {
		return nil, fmt.Errorf("VpcCidr must be specified")
	}

	pr := skysetup.Spec.ProviderRef
	if pr.ProviderName == "" &&
		pr.ProviderType == "" &&
		pr.ProviderRegion == "" &&
		pr.ProviderZone == "" {
		return nil, fmt.Errorf("ProviderRef must be specified")
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type SkySetup.
func (v *SkySetupCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	skysetup, ok := newObj.(*svcv1alpha1.SkySetup)
	if !ok {
		return nil, fmt.Errorf("expected a SkySetup object for the newObj but got %T", newObj)
	}
	skysetuplog.Info("Validation for SkySetup upon update", "name", skysetup.GetName())

	if skysetup.Spec.ForProvider.VpcCidr == "" {
		return nil, fmt.Errorf("VpcCidr must be specified")
	}

	pr := skysetup.Spec.ProviderRef
	if pr.ProviderName == "" &&
		pr.ProviderType == "" &&
		pr.ProviderRegion == "" &&
		pr.ProviderZone == "" {
		return nil, fmt.Errorf("ProviderRef must be specified")
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type SkySetup.
func (v *SkySetupCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	skysetup, ok := obj.(*svcv1alpha1.SkySetup)
	if !ok {
		return nil, fmt.Errorf("expected a SkySetup object but got %T", obj)
	}
	skysetuplog.Info("Validation for SkySetup upon deletion", "name", skysetup.GetName())
	return nil, nil
}
