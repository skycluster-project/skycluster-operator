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

	lo "github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	"github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

// nolint:unused
// log is for logging in this package.
var imgLogger = zap.New(helper.CustomLogger()).WithName("[Images Defaulting]")

// SetupImageWebhookWithManager registers the webhook for Image in the manager.
func SetupImageWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&cv1a1.Image{}).
		WithValidator(&ImageCustomValidator{client: mgr.GetClient()}).
		WithDefaulter(&ImageCustomDefaulter{client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-skycluster-io-v1alpha1-image,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=images,verbs=create;update,versions=v1alpha1,name=mimage-v1alpha1.kb.io,admissionReviewVersions=v1

// ImageCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Image when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type ImageCustomDefaulter struct {
	client client.Client
}

var _ webhook.CustomDefaulter = &ImageCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Image.
func (d *ImageCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	img, ok := obj.(*cv1a1.Image)

	if !ok {
		return fmt.Errorf("expected an Image object but got %T", obj)
	}
	imgLogger.Info("Defaulting for Image", "name", img.GetName())

	provider := &cv1a1.ProviderProfile{}
	if err := d.client.Get(context.Background(), client.ObjectKey{Name: img.Spec.ProviderRef, Namespace: img.Namespace}, provider); err != nil {
		imgLogger.Info("unable to fetch ProviderProfile for image", "name", img.Spec.ProviderRef)
		return client.IgnoreNotFound(err)
	}

	// Update the Images resource with the new labels
	img.Labels = d.addUpdateDefaultLabels(img, provider)

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-skycluster-io-v1alpha1-image,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=images,verbs=create;update,versions=v1alpha1,name=vimage-v1alpha1.kb.io,admissionReviewVersions=v1

// ImageCustomValidator struct is responsible for validating the Image resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ImageCustomValidator struct {
	client client.Client
}

var _ webhook.CustomValidator = &ImageCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Image.
func (v *ImageCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	img, ok := obj.(*cv1a1.Image)
	if !ok {
		return nil, fmt.Errorf("expected a Image object but got %T", obj)
	}
	imgLogger.Info("Validation for Image upon creation", "name", img.GetName())

	provider := &cv1a1.ProviderProfile{}
	if err := v.client.Get(context.Background(), client.ObjectKey{Name: img.Spec.ProviderRef, Namespace: img.Namespace}, provider); err != nil {
		imgLogger.Info("unable to fetch ProviderProfile for image", "name", img.Spec.ProviderRef)
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Image.
func (v *ImageCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	image, ok := newObj.(*cv1a1.Image)
	if !ok {
		return nil, fmt.Errorf("expected a Image object for the newObj but got %T", newObj)
	}
	imgLogger.Info("Validation for Image upon update", "name", image.GetName())

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Image.
func (v *ImageCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	image, ok := obj.(*cv1a1.Image)
	if !ok {
		return nil, fmt.Errorf("expected a Image object but got %T", obj)
	}
	imgLogger.Info("Validation for Image upon deletion", "name", image.GetName())

	return nil, nil
}

func (d *ImageCustomDefaulter) addUpdateDefaultLabels(img *cv1a1.Image, provider *cv1a1.ProviderProfile) map[string]string {
	if img.Labels == nil {
		img.Labels = make(map[string]string)
	}

	defaultZone, ok := lo.Find(provider.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	if ok {
		img.Labels["skycluster.io/provider-zone"] = defaultZone.Name
	}
	img.Labels["skycluster.io/provider-platform"] = provider.Spec.Platform
	img.Labels["skycluster.io/provider-region"] = provider.Spec.Region
	return img.Labels
}
