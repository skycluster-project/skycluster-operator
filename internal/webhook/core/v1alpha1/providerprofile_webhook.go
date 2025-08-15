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
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

// nolint:unused
// log is for logging in this package.
var ppLogger = zap.New(pkglog.CustomLogger()).WithName("[ProviderProfile Webhook]")

// SetupProviderProfileWebhookWithManager registers the webhook for ProviderProfile in the manager.
func SetupProviderProfileWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&cv1a1.ProviderProfile{}).
		WithValidator(&ProviderProfileCustomValidator{client: mgr.GetClient()}).
		WithDefaulter(&ProviderProfileCustomDefaulter{client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-skycluster-io-v1alpha1-providerprofile,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=providerprofiles,verbs=create;update,versions=v1alpha1,name=mproviderprofile-v1alpha1.kb.io,admissionReviewVersions=v1

// ProviderProfileCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind ProviderProfile when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type ProviderProfileCustomDefaulter struct {
	client client.Client
}

var _ webhook.CustomDefaulter = &ProviderProfileCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind ProviderProfile.
func (d *ProviderProfileCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	pp, ok := obj.(*cv1a1.ProviderProfile)

	if !ok {
		return fmt.Errorf("expected an ProviderProfile object but got %T", obj)
	}
	ppLogger.Info("Defaulting for ProviderProfile", "name", pp.GetName())

	pp.Labels = d.addUpdateDefaultLabels(pp)

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-skycluster-io-v1alpha1-providerprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=providerprofiles,verbs=create;update,versions=v1alpha1,name=vproviderprofile-v1alpha1.kb.io,admissionReviewVersions=v1

// ProviderProfileCustomValidator struct is responsible for validating the ProviderProfile resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ProviderProfileCustomValidator struct {
	client client.Client
}

var _ webhook.CustomValidator = &ProviderProfileCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ProviderProfile.
func (v *ProviderProfileCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	providerprofile, ok := obj.(*cv1a1.ProviderProfile)
	if !ok {
		return nil, fmt.Errorf("expected a ProviderProfile object but got %T", obj)
	}
	ppLogger.Info("Validation for ProviderProfile upon creation", "name", providerprofile.GetName())

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ProviderProfile.
func (v *ProviderProfileCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	providerprofile, ok := newObj.(*cv1a1.ProviderProfile)
	if !ok {
		return nil, fmt.Errorf("expected a ProviderProfile object for the newObj but got %T", newObj)
	}
	ppLogger.Info("Validation for ProviderProfile upon update", "name", providerprofile.GetName())

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ProviderProfile.
func (v *ProviderProfileCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	providerprofile, ok := obj.(*cv1a1.ProviderProfile)
	if !ok {
		return nil, fmt.Errorf("expected a ProviderProfile object but got %T", obj)
	}
	ppLogger.Info("Validation for ProviderProfile upon deletion", "name", providerprofile.GetName())

	return nil, nil
}

func (d *ProviderProfileCustomDefaulter) addUpdateDefaultLabels(pp *cv1a1.ProviderProfile) map[string]string {
	if pp.Labels == nil {
		pp.Labels = make(map[string]string)
	}

	defaultZone, ok := lo.Find(pp.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	if ok {
		pp.Labels["skycluster.io/provider-zone"] = defaultZone.Name
	}
	pp.Labels["skycluster.io/managed-by"] = "skycluster"
	pp.Labels["skycluster.io/provider-platform"] = pp.Spec.Platform
	pp.Labels["skycluster.io/provider-region"] = pp.Spec.Region
	return pp.Labels
}

// func (r *ProviderProfileCustomValidator) cleanUp(ctx context.Context, pf *corev1alpha1.ProviderProfile) error {

// 	// Clean up ConfigMaps related to the provider profile
// 	p := pf.Spec.Platform
// 	rg := pf.Spec.Region
// 	defaultZone, ok := lo.Find(pf.Spec.Zones, func(zone corev1alpha1.ZoneSpec) bool {
// 		return zone.DefaultZone
// 	})
// 	if !ok {
// 		return fmt.Errorf("no default zone found for provider profile %s", pf.Name)
// 	}
// 	cm := &corev1.ConfigMapList{}
// 	// List all ConfigMaps in the namespace with matching labels
// 	if err := r.List(ctx, cm, &client.ListOptions{
// 		Namespace: helper.SKYCLUSTER_NAMESPACE,
// 		LabelSelector: labels.SelectorFromSet(labels.Set{
// 			"skycluster.io/provider-platform": p,
// 			"skycluster.io/provider-region":   rg,
// 			"skycluster.io/provider-zone":     defaultZone.Name,
// 			"skycluster.io/config-type":       "provider-profile",
// 		}),
// 	}); err != nil {
// 		return fmt.Errorf("unable to list ConfigMaps for cleanup: %w", err)
// 	}
// 	if len(cm.Items) > 1 {
// 		return fmt.Errorf("multiple ConfigMaps found for provider profile %s, expected only one", pf.Name)
// 	}
// 	// Delete each ConfigMap found
// 	for _, cmItem := range cm.Items {
// 		if err := r.Delete(ctx, &cmItem); err != nil {
// 			return fmt.Errorf("unable to delete ConfigMap %s during cleanup: %w", cmItem.Name, err)
// 		}
// 	}

// 	return nil
// }
