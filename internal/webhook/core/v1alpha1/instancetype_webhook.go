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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

// nolint:unused

// SetupInstanceTypeWebhookWithManager registers the webhook for InstanceType in the manager.
func SetupInstanceTypeWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&cv1a1.InstanceType{}).
		WithValidator(&InstanceTypeCustomValidator{
			client: mgr.GetClient(),
			logger: zap.New(pkglog.CustomLogger()).WithName("[InstanceTypeWebhook]"),
		}).
		WithDefaulter(&InstanceTypeCustomDefaulter{
			client: mgr.GetClient(),
			logger: zap.New(pkglog.CustomLogger()).WithName("[InstanceTypeWebhook]"),
		}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-skycluster-io-v1alpha1-instancetype,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=instancetypes,verbs=create;update,versions=v1alpha1,name=minstancetype-v1alpha1.kb.io,admissionReviewVersions=v1

// InstanceTypeCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind InstanceType when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type InstanceTypeCustomDefaulter struct {
	client client.Client
	logger logr.Logger
}

var _ webhook.CustomDefaulter = &InstanceTypeCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind InstanceType.
func (d *InstanceTypeCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	it, ok := obj.(*cv1a1.InstanceType)

	if !ok {
		return fmt.Errorf("expected an InstanceType object but got %T", obj)
	}
	
	provider := &cv1a1.ProviderProfile{}
	if err := d.client.Get(context.Background(), client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, provider); err != nil {
		d.logger.Info("unable to fetch ProviderProfile for image", "name", it.Spec.ProviderRef)
		return client.IgnoreNotFound(err)
	}

	// Update the Images resource with the new labels
	it.Labels = d.addUpdateDefaultLabels(it, provider)

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-skycluster-io-v1alpha1-instancetype,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=instancetypes,verbs=create;update,versions=v1alpha1,name=vinstancetype-v1alpha1.kb.io,admissionReviewVersions=v1

// InstanceTypeCustomValidator struct is responsible for validating the InstanceType resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type InstanceTypeCustomValidator struct {
	client client.Client
	logger logr.Logger
}

var _ webhook.CustomValidator = &InstanceTypeCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type InstanceType.
func (v *InstanceTypeCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	it, ok := obj.(*cv1a1.InstanceType)
	if !ok {
		return nil, fmt.Errorf("expected a InstanceType object but got %T", obj)
	}

	provider := &cv1a1.ProviderProfile{}
	if err := v.client.Get(context.Background(), client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, provider); err != nil {
		v.logger.Info("unable to fetch ProviderProfile for instance type", "name", it.Spec.ProviderRef)
		return nil, err
	}
	if it.Namespace != provider.Namespace {
		return nil, fmt.Errorf("instance type %s/%s references a provider profile %s/%s in a different namespace", it.Namespace, it.Name, provider.Namespace, provider.Name)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type InstanceType.
func (v *InstanceTypeCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	instancetype, ok := newObj.(*cv1a1.InstanceType)
	if !ok {
		return nil, fmt.Errorf("expected a InstanceType object for the newObj but got %T", newObj)
	}
	v.logger.Info("Validation for InstanceType upon update", "name", instancetype.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type InstanceType.
func (v *InstanceTypeCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	instancetype, ok := obj.(*cv1a1.InstanceType)
	if !ok {
		return nil, fmt.Errorf("expected a InstanceType object but got %T", obj)
	}
	v.logger.Info("Validation for InstanceType upon deletion", "name", instancetype.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

func (d *InstanceTypeCustomDefaulter) addUpdateDefaultLabels(it *cv1a1.InstanceType, provider *cv1a1.ProviderProfile) map[string]string {
	if it.Labels == nil {
		it.Labels = make(map[string]string)
	}

	it.Labels["skycluster.io/provider-platform"] = provider.Spec.Platform
	it.Labels["skycluster.io/provider-region"] = provider.Spec.Region
	it.Labels["skycluster.io/provider-profile"] = provider.Name
	it.Labels["skycluster.io/managed-by"] = "skycluster"
	return it.Labels
}
