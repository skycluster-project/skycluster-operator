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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

// LatencyReconciler reconciles a Latency object
type LatencyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=latencies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=latencies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=latencies/finalizers,verbs=update

func (r *LatencyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("Latency", req.NamespacedName.String())
	logger.Info("Reconciling Latency started.")
	var lat cv1a1.Latency
	if err := r.Get(ctx, req.NamespacedName, &lat); err != nil {
		// ignore not found
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Update status with last measured values
	if lat.Status.LastMeasuredMs == "" || lat.Spec.FixedLatencyMs != lat.Status.LastMeasuredMs {
		lat.Status.LastMeasuredMs = lat.Spec.FixedLatencyMs
		lat.Status.P95 = lat.Spec.FixedLatencyMs
		lat.Status.P99 = lat.Spec.FixedLatencyMs
	}
	err := r.Status().Update(ctx, &lat)
	if err != nil {
		logger.Error(err, "Failed to update Latency status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LatencyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.Latency{}).
		Named("core-latency").
		Complete(r)
}
