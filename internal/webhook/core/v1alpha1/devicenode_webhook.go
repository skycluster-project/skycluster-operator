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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/go-logr/logr"
	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

// nolint:unused

// SetupDeviceNodeWebhookWithManager registers the webhook for DeviceNode in the manager.
func SetupDeviceNodeWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&cv1a1.DeviceNode{}).
		WithValidator(&DeviceNodeCustomValidator{
			client: mgr.GetClient(),
			logger: zap.New(pkglog.CustomLogger()).WithName("[DeviceNodeWebhook]"),
		}).
		WithDefaulter(&DeviceNodeCustomDefaulter{
			client: mgr.GetClient(),
			logger: zap.New(pkglog.CustomLogger()).WithName("[DeviceNodeWebhook]"),
		}).
		Complete()
}


// +kubebuilder:webhook:path=/mutate-core-skycluster-io-v1alpha1-devicenode,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=devicenodes,verbs=create;update,versions=v1alpha1,name=mdevicenode-v1alpha1.kb.io,admissionReviewVersions=v1

// DeviceNodeCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind DeviceNode when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type DeviceNodeCustomDefaulter struct {
	client client.Client
	logger logr.Logger
}

var _ webhook.CustomDefaulter = &DeviceNodeCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind DeviceNode.
func (d *DeviceNodeCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	dn, ok := obj.(*cv1a1.DeviceNode)

	if !ok {
		return fmt.Errorf("expected an DeviceNode object but got %T", obj)
	}

	provider := &cv1a1.ProviderProfile{}
	if err := d.client.Get(context.Background(), client.ObjectKey{Name: dn.Spec.ProviderRef, Namespace: dn.Namespace}, provider); err != nil {
		d.logger.Info("unable to fetch ProviderProfile for DeviceNode", "name", dn.Spec.ProviderRef)
		return client.IgnoreNotFound(err)
	}

	// Update the Images resource with the new labels
	dn.Labels = d.addUpdateDefaultLabels(dn, provider)

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-skycluster-io-v1alpha1-devicenode,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.skycluster.io,resources=devicenodes,verbs=create;update,versions=v1alpha1,name=vdevicenode-v1alpha1.kb.io,admissionReviewVersions=v1

// DeviceNodeCustomValidator struct is responsible for validating the DeviceNode resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type DeviceNodeCustomValidator struct {
	client client.Client
	logger logr.Logger
}

var _ webhook.CustomValidator = &DeviceNodeCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type DeviceNode.
func (v *DeviceNodeCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	dn, ok := obj.(*cv1a1.DeviceNode)
	if !ok {
		return nil, fmt.Errorf("expected a DeviceNode object but got %T", obj)
	}
	v.logger.Info("Validation for DeviceNode upon creation", "name", dn.GetName())

	provider := &cv1a1.ProviderProfile{}
	if err := v.client.Get(context.Background(), client.ObjectKey{Name: dn.Spec.ProviderRef, Namespace: dn.Namespace}, provider); err != nil {
		return nil, fmt.Errorf("unable to fetch ProviderProfile %q for DeviceNode %q: %w", dn.Spec.ProviderRef, dn.Name, err)
	}

	if dn.Namespace != provider.Namespace {
		return nil, fmt.Errorf("ProviderProfile %q is not in the same namespace as DeviceNode %q", dn.Spec.ProviderRef, dn.Name)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type DeviceNode.
func (v *DeviceNodeCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	devicenode, ok := newObj.(*cv1a1.DeviceNode)
	if !ok {
		return nil, fmt.Errorf("expected a DeviceNode object for the newObj but got %T", newObj)
	}
	v.logger.Info("Validation for DeviceNode upon update", "name", devicenode.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type DeviceNode.
func (v *DeviceNodeCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	devicenode, ok := obj.(*cv1a1.DeviceNode)
	if !ok {
		return nil, fmt.Errorf("expected a DeviceNode object but got %T", obj)
	}
	v.logger.Info("Validation for DeviceNode upon deletion", "name", devicenode.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}



func (d *DeviceNodeCustomDefaulter) addUpdateDefaultLabels(dn *cv1a1.DeviceNode, provider *cv1a1.ProviderProfile) map[string]string {
	if dn.Labels == nil {
		dn.Labels = make(map[string]string)
	}

	dn.Labels["skycluster.io/provider-platform"] = provider.Spec.Platform
	dn.Labels["skycluster.io/provider-region"] = provider.Spec.Region
	dn.Labels["skycluster.io/provider-profile"] = provider.Name
	dn.Labels["skycluster.io/managed-by"] = "skycluster"
	return dn.Labels
}
