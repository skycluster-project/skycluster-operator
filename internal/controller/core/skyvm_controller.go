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

package core

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/google/uuid"
)

// SkyVMReconciler reconciles a SkyVM object
type SkyVMReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyvms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyvms/finalizers,verbs=update

func (r *SkyVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the object
	var obj corev1alpha1.SkyVM
	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		logger.Error(err, "unable to fetch object, maybe it is deleted?")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// add essential labels
	modified := r.updateLabels(&obj, map[string]string{
		"skycluster.io/provider-name":   obj.Spec.ProviderRef.ProviderName,
		"skycluster.io/provider-region": obj.Spec.ProviderRef.ProviderRegion,
		"skycluster.io/provider-zone":   obj.Spec.ProviderRef.ProviderZone,
		"skycluster.io/project-id":      uuid.New().String(),
	})

	// SkyVM depends on SkyProvider
	providerExists, err := r.skyProviderExists(ctx, &obj)
	if err != nil {
		logger.Error(err, "failed to check if SkyProvider exists")
		return ctrl.Result{}, err
	}
	if !providerExists {
		logger.Info("SkyProvider does not exists", "exists", providerExists)
		// if err := r.createSkyProvider(ctx, &obj); err != nil {
		// 	logger.Error(err, "failed to create SkyProvider")
		// 	return ctrl.Result{}, err
		// }
	}

	if modified {
		if err := r.Update(ctx, &obj); err != nil {
			logger.Error(err, "failed to update object with project-id")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *SkyVMReconciler) skyProviderExists(ctx context.Context, obj *corev1alpha1.SkyVM) (bool, error) {
	logger := log.FromContext(ctx)
	providerRef := obj.Spec.ProviderRef
	// search through all SkyProviders and compare the provider-name and provider-region fields
	skyProviders := &corev1alpha1.SkyProviderList{}
	listOptions := &client.ListOptions{
		Namespace: obj.Namespace,
	}
	if err := r.List(ctx, skyProviders, listOptions); err != nil {
		logger.Error(err, "failed to list SkyProviders")
		return false, err
	}
	// Search for the first SkyProvider with matching provider-name
	for _, skyProvider := range skyProviders.Items {
		nameMatch := skyProvider.Spec.ProviderRef.ProviderName == providerRef.ProviderName
		regionMatch := skyProvider.Spec.ProviderRef.ProviderRegion == providerRef.ProviderRegion
		zoneMatch := skyProvider.Spec.ProviderRef.ProviderZone == providerRef.ProviderZone
		if nameMatch && regionMatch && zoneMatch {
			return true, nil
		}
	}

	return false, nil
}

func (r *SkyVMReconciler) createSkyProvider(ctx context.Context, obj *corev1alpha1.SkyVM) error {
	logger := log.FromContext(ctx)

	skyProvider := &corev1alpha1.SkyProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "skyprovider-" + obj.Name,
			Namespace: obj.Namespace,
		},
		Spec: corev1alpha1.SkyProviderSpec{
			ProviderRef: obj.Spec.ProviderRef,
		},
	}

	// Set SkyVM instance as the owner and controller
	if err := ctrl.SetControllerReference(obj, skyProvider, r.Scheme); err != nil {
		logger.Error(err, "failed to set owner reference")
		return err
	}

	// Check if the SkyProvider object already exists
	if err := r.Get(ctx, client.ObjectKeyFromObject(skyProvider), skyProvider); err != nil {
		if err := r.Create(ctx, skyProvider); err != nil {
			logger.Error(err, "failed to create SkyProvider")
			return err
		}
	}

	return nil
}

func (r *SkyVMReconciler) updateLabels(obj *corev1alpha1.SkyVM, labels map[string]string) bool {
	modified := false
	if obj.Labels == nil {
		obj.Labels = make(map[string]string)
	}
	for key, value := range labels {
		vv, exists := obj.Labels[key]
		if !exists || vv != value {
			obj.Labels[key] = value
			modified = true
		}
	}
	return modified
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyVM{}).
		Named("core-skyvm").
		Complete(r)
}
