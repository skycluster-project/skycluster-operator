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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

// DeviceNodeReconciler reconciles a DeviceNode object
type DeviceNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=devicenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=devicenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=devicenodes/finalizers,verbs=update

func (r *DeviceNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(pkglog.CustomLogger()).WithName("[DeviceNode]")
	logger.Info("Reconciler started.", "name", req.Name)

	// Fetch the DeviceNode instance
	deviceNode := &cv1a1.DeviceNode{}
	if err := r.Get(ctx, req.NamespacedName, deviceNode); err != nil {
		logger.Info("unable to fetch DeviceNode")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Based on providerRef, fetch the provider details
	provider := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: deviceNode.Spec.ProviderRef, Namespace: deviceNode.Namespace}, provider); err != nil {
		logger.Error(err, "failed to fetch ProviderProfile", "providerRef", deviceNode.Spec.ProviderRef)
		return ctrl.Result{}, err
	}

	// If the provider platform is aws, gcp or azure, we ignore the device node creation
	if provider.Spec.Platform == "aws" || provider.Spec.Platform == "gcp" || provider.Spec.Platform == "azure" {
		logger.Info("Ignoring DeviceNode creation for cloud providers", "platform", provider.Spec.Platform)
		return ctrl.Result{}, nil
	}

	// For each device spec, if the zone does not exist, we ignore the device node, otherwise
	// we copy the device spec to the status
	observedDevices := make([]cv1a1.DeviceSpec, 0)
	for _, deviceSpec := range deviceNode.Spec.DeviceSpec {
		if !checkZone(deviceSpec.ZoneRef, provider) {
			logger.Info("Ignoring DeviceNode creation for non-existing zone", "zone", deviceSpec.ZoneRef)
			continue
		}
		// If the zone exists, we copy the device spec to the status
		observedDevices = append(observedDevices, deviceSpec)
	}

	if len(observedDevices) == 0 {
		logger.Info("No valid device specs found, skipping DeviceNode creation")
		return ctrl.Result{}, nil
	}

	deviceNode.Status.DevicesSpec = observedDevices
	deviceNode.Status.Region = provider.Spec.Region

	// Update the DeviceNode status
	if err := r.Status().Update(ctx, deviceNode); err != nil {
		logger.Error(err, "failed to update DeviceNode status")
		return ctrl.Result{}, err
	}

	// Add provider labels to the DeviceNode
	if deviceNode.Labels == nil {
		deviceNode.Labels = make(map[string]string)
	}
	deviceNode.Labels["skycluster.io/provider-platform"] = provider.Spec.Platform
	deviceNode.Labels["skycluster.io/provider-region"] = provider.Spec.Region
	// Update the DeviceNode with the new labels
	if err := r.Update(ctx, deviceNode); err != nil {
		logger.Error(err, "failed to update DeviceNode labels")
		return ctrl.Result{}, err
	}

	logger.Info("DeviceNode reconciled successfully", "name", deviceNode.Name)
	return ctrl.Result{}, nil
}

func checkZone(zoneName string, provider *cv1a1.ProviderProfile) bool {
	// iterate over the zones in the provider and check if the zone exists
	for _, zone := range provider.Spec.Zones {
		if zone.Name == zoneName {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeviceNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.DeviceNode{}).
		Named("core-devicenode").
		Complete(r)
}
