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

	svcv1alpha1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var skyproviderlog = logf.Log.WithName("skyprovider-resource")

// SetupSkyProviderWebhookWithManager registers the webhook for SkyProvider in the manager.
func SetupSkyProviderWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&svcv1alpha1.SkyProvider{}).
		WithValidator(&SkyProviderCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-svc-skycluster-io-v1alpha1-skyprovider,mutating=false,failurePolicy=fail,sideEffects=None,groups=svc.skycluster.io,resources=skyproviders,verbs=create;update,versions=v1alpha1,name=vskyprovider-v1alpha1.kb.io,admissionReviewVersions=v1

// SkyProviderCustomValidator struct is responsible for validating the SkyProvider resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type SkyProviderCustomValidator struct{}

var _ webhook.CustomValidator = &SkyProviderCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type SkyProvider.
func (v *SkyProviderCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	skyprovider, ok := obj.(*svcv1alpha1.SkyProvider)
	if !ok {
		return nil, fmt.Errorf("expected a SkyProvider object but got %T", obj)
	}
	skyproviderlog.Info(fmt.Sprintf("[%s]\tValidation for SkyProvider upon creation", skyprovider.GetName()))

	if skyprovider.Spec.ProviderGateway.VpcCidr == "" {
		return nil, fmt.Errorf("VpcCidr must be specified")
	}

	pr := skyprovider.Spec.ProviderRef
	if pr.ProviderName == "" &&
		pr.ProviderType == "" &&
		pr.ProviderRegion == "" &&
		pr.ProviderZone == "" {
		return nil, fmt.Errorf("ProviderRef must be specified")
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type SkyProvider.
func (v *SkyProviderCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	skyprovider, ok := newObj.(*svcv1alpha1.SkyProvider)
	if !ok {
		return nil, fmt.Errorf("expected a SkyProvider object for the newObj but got %T", newObj)
	}
	skyproviderlog.Info(fmt.Sprintf("[%s]\tValidation for SkyProvider upon update", skyprovider.GetName()))

	return nil, fmt.Errorf("SkyProvider is immutable!")
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type SkyProvider.
func (v *SkyProviderCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	_, ok := obj.(*svcv1alpha1.SkyProvider)
	if !ok {
		return nil, fmt.Errorf("expected a SkyProvider object but got %T", obj)
	}
	return nil, nil
}
