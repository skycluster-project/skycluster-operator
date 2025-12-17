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
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	svcv1a1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
)

// SetupXInstanceWebhookWithManager registers the webhook for XInstance in the manager.
func SetupXInstanceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&svcv1a1.XInstance{}).
		WithDefaulter(&XInstanceCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-svc-skycluster-io-v1alpha1-xinstance,mutating=true,failurePolicy=fail,sideEffects=None,groups=svc.skycluster.io,resources=xinstances,verbs=create;update,versions=v1alpha1,name=mxinstance-v1alpha1.kb.io,admissionReviewVersions=v1

// XInstanceCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind XInstance when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type XInstanceCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &XInstanceCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind XInstance.
func (d *XInstanceCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	_, ok := obj.(*svcv1a1.XInstance)
	if !ok {
		return fmt.Errorf("expected an XInstance object but got %T", obj)
	}
	return nil
}
