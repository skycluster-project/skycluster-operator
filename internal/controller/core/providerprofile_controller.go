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
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	helper "github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

const (
	cmFinalizer  = "skycluster.io/configmap"
	requeueAfter = 2 * time.Second
)

var logger = zap.New(helper.CustomLogger()).WithName("[ProviderProfile]")

// ProviderProfileReconciler reconciles a ProviderProfile object
type ProviderProfileReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/finalizers,verbs=update

// Reconcile behavior:
// 1. If it's being deleted, clean up config maps and remove finalizer.
// 2. If it's being created or updated, copy spec to status and create a ConfigMap if it doesn't exist.

func (r *ProviderProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	logger.Info(fmt.Sprintf("Reconciler started for %s", req.Name))

	// Copy all values from spec to status
	pp := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, req.NamespacedName, pp); err != nil {
		logger.Info("unable to fetch ProviderProfile, may be deleted", "name", req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If object is being deleted
	if !pp.DeletionTimestamp.IsZero() {
		logger.Info("ProviderProfile is being deleted", "name", req.Name)
		if controllerutil.ContainsFinalizer(pp, cmFinalizer) {
			logger.Info("ProviderProfile has finalizer, cleaning up ConfigMaps", "name", req.Name)
			if cms, err := r.getConfigMap(ctx, pp); err == nil {
				// if no error, then there is a ConfigMap to clean up
				if err := r.cleanUp(ctx, cms); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to clean up ConfigMaps: %w", err)
				}
				// Requeue to ensure finalizer is removed
				return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
			}

			// There is no ConfigMap, Remove finalizer once cleanup is done
			controllerutil.RemoveFinalizer(pp, cmFinalizer)
			logger.Info("Removing finalizer from ProviderProfile", "name", req.Name)
			if err := r.Update(ctx, pp); err != nil {
				logger.Error(err, "unable to remove finalizer from ProviderProfile")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Create a ConfigMap if it doesn't exist
	cms, err := r.getConfigMap(ctx, pp)
	if err != nil && !controllerutil.ContainsFinalizer(pp, cmFinalizer) {
		logger.Info("Creating ConfigMap for ProviderProfile, no finalizer present", "name", req.Name)
		cm, err := r.createConfigMap(ctx, pp, req.Name)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "unable to create ConfigMap for ProviderProfile")
			return ctrl.Result{}, err
		}
		logger.Info("ConfigMap created for ProviderProfile", "name", cm.Name)
		logger.Info("Adding finalizer to ProviderProfile", "name", req.Name)
		controllerutil.AddFinalizer(pp, cmFinalizer)
		logger.Info("Updating ProviderProfile with finalizer", "name", req.Name)
		if err := r.Update(ctx, pp); err != nil {
			logger.Error(err, "unable to add finalizer to ProviderProfile")
			return ctrl.Result{}, err
		}
	}

	logger.Info("Setting ProviderProfile status", "name", req.Name)
	// Copy all spec values to status
	if cms != nil {
		pp.Status.ConfigMapRef = strings.Join(
			lo.Map(cms.Items, func(n corev1.ConfigMap, _ int) string { return n.Name }), ",")
	}
	pp.Status.Enabled = pp.Spec.Enabled
	pp.Status.Zones = make([]cv1a1.ZoneSpec, len(pp.Spec.Zones))
	pp.Status.Zones = lo.Filter(pp.Spec.Zones, func(zone cv1a1.ZoneSpec, _ int) bool { return true })

	// Update the status with the current spec
	logger.Info("Updating ProviderProfile status", "name", req.Name)
	if err := r.Status().Update(ctx, pp); err != nil {
		if apierrors.IsConflict(err) {
			logger.Info("Conflict while updating ProviderProfile status, requeuing", "name", req.Name)
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}
		logger.Error(err, "unable to update ProviderProfile status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.ProviderProfile{}).
		Named("core-providerprofile").
		Complete(r)
}

func (r *ProviderProfileReconciler) createConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile, name string) (*corev1.ConfigMap, error) {
	defaultZone, ok := lo.Find(pf.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	ll := helper.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, lo.Ternary(ok, defaultZone.Name, ""))
	logger.Info("Creating ConfigMap for ProviderProfile", "name", name, "labels", ll)
	// Create a new ConfigMap for the ProviderProfile
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: helper.SKYCLUSTER_NAMESPACE,
			Name:      fmt.Sprintf("profile-%s", name),
			Labels:    labels.Set(ll),
		},
	}

	// Create the ConfigMap
	if err := r.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("unable to create ConfigMap: %w", err)
	}

	return cm, nil
}

func (r *ProviderProfileReconciler) getConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile) (*corev1.ConfigMapList, error) {

	defaultZone, ok := lo.Find(pf.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	ll := helper.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, lo.Ternary(ok, defaultZone.Name, ""))

	// Get the ConfigMap associated with the provider profile
	cms := &corev1.ConfigMapList{}
	if err := r.List(ctx, cms, &client.ListOptions{
		Namespace:     helper.SKYCLUSTER_NAMESPACE,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch ConfigMap for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(cms.Items) == 0 {
		return nil, fmt.Errorf("no ConfigMap found for ProviderProfile %s", pf.Name)
	}
	return cms, nil
}

func (r *ProviderProfileReconciler) cleanUp(ctx context.Context, cms *corev1.ConfigMapList) error {
	// Clean up ConfigMaps related to the provider profile
	for _, cmItem := range cms.Items {
		if err := r.Delete(ctx, &cmItem); err != nil {
			return fmt.Errorf("unable to delete ConfigMap %s during cleanup: %w", cmItem.Name, err)
		}
	}

	return nil
}
