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
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

// DeviceNodeReconciler reconciles a DeviceNode object
type DeviceNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger  logr.Logger
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=devicenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=devicenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=devicenodes/finalizers,verbs=update

func (r *DeviceNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started.", "name", req.Name)

	// Fetch the DeviceNode instance
	dn := &cv1a1.DeviceNode{}
	if err := r.Get(ctx, req.NamespacedName, dn); err != nil {
		r.Logger.Info("unable to fetch DeviceNode")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Based on providerRef, fetch the provider details
	provider := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: dn.Spec.ProviderRef, Namespace: dn.Namespace}, provider); err != nil {
		r.Logger.Error(err, "failed to fetch ProviderProfile", "providerRef", dn.Spec.ProviderRef)
		return ctrl.Result{}, err
	}

	// If the provider platform is aws, gcp or azure, we ignore the device node creation
	if !lo.Contains([]string{"baremetal"}, strings.ToLower(provider.Spec.Platform)) {
		return ctrl.Result{}, nil
	}

	// if zone is not supported, return
	pZones := lo.Map(provider.Spec.Zones, func(z cv1a1.ZoneSpec, _ int) string {return z.Name})
	if !lo.Contains(pZones, strings.ToLower(dn.Spec.Zone)) {return ctrl.Result{}, nil}

	dn.Status.Region = provider.Spec.Region
	dn.Status.Zone = dn.Spec.Zone

	// Update the DeviceNode status
	if err := r.Status().Update(ctx, dn); err != nil {
		r.Logger.Error(err, "failed to update DeviceNode status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeviceNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.DeviceNode{}).
		Named("core-devicenode").
		Complete(r)
}
