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

	"gopkg.in/yaml.v3"
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

// 2. If it's being created:
// 		- copy spec to status
// 		- create a ConfigMap

// 3. If it exists (update only):
// 	  (the update should be triggered by the image and instance type reconcilers)
//    - copy spec to status
//    - check if ConfigMap exists, if not create it
//    - pull data from ConfigMap and update status

func (r *ProviderProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var logger = zap.New(helper.CustomLogger()).WithName("[ProviderProfile]")
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
				logger.Info("Found ConfigMap for ProviderProfile, cleaning up", "name", req.Name, "Found ConfigMaps", len(cms.Items))
				if err := r.cleanUp(ctx, cms); err != nil {
					logger.Error(err, "failed to clean up ConfigMaps for ProviderProfile", "name", req.Name)
					return ctrl.Result{}, fmt.Errorf("failed to clean up ConfigMaps: %w", err)
				}
				logger.Info("ConfigMaps cleaned up for ProviderProfile", "name", req.Name)
				// Requeue to ensure finalizer is removed
				return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
			} else {
				// If no ConfigMap exists, we can proceed to remove the finalizer
				logger.Info("No ConfigMap found for ProviderProfile", "name", req.Name, "error", err)
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
	pp.Status.Region = pp.Spec.Region
	pp.Status.Zones = make([]cv1a1.ZoneSpec, len(pp.Spec.Zones))
	pp.Status.Zones = lo.Filter(pp.Spec.Zones, func(zone cv1a1.ZoneSpec, _ int) bool { return true })
	totalServices, err := getNumberOfServices(cms)
	if err != nil {
		logger.Error(err, "unable to get number of services for ProviderProfile", "name", req.Name)
		pp.Status.Sync = false // TODO: Requeue for retry?
		// continue updating the status even if we can't get the number of services
	} else {
		pp.Status.TotalServices = totalServices
		pp.Status.Sync = true
	}

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
	var logger = zap.New(helper.CustomLogger()).WithName("[ProviderProfile]")
	// defaultZone, ok := lo.Find(pf.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
	// 	return zone.DefaultZone
	// })
	ll := helper.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
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

	// defaultZone, ok := lo.Find(pf.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
	// 	return zone.DefaultZone
	// })
	ll := helper.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")

	// Get the ConfigMap associated with the provider profile
	cms := &corev1.ConfigMapList{}
	if err := r.List(ctx, cms, &client.ListOptions{
		Namespace:     helper.SKYCLUSTER_NAMESPACE,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch ConfigMap for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(cms.Items) == 0 {
		return nil, fmt.Errorf("no ConfigMap found for ProviderProfile %s, labels: %v", pf.Name, ll)
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

func getNumberOfServices(cms *corev1.ConfigMapList) (int, error) {
	var logger = zap.New(helper.CustomLogger()).WithName("[ProviderProfile]")
	if cms == nil || len(cms.Items) == 0 {
		return 0, fmt.Errorf("no ConfigMap found")
	}

	// Assuming each ConfigMap contains a list of services in a specific key
	logger.Info("Counting services from ConfigMaps", "Found ConfigMaps", len(cms.Items))
	serviceCount := 0
	for _, cm := range cms.Items {
		for svc, yamlData := range cm.Data { // svcData["images.yaml"]
			logger.Info("Decoding YAML data from ConfigMap", "ConfigMap", cm.Name)
			svcData, err := decodeSvcYaml(yamlData)
			if err != nil {
				return 0, fmt.Errorf("failed to decode YAML in ConfigMap %s: %w", cm.Name, err)
			}
			logger.Info("Decoded service data from ConfigMap", "ConfigMap", cm.Name, "Service", svc, "Data", len(svcData))
			switch svc {
			case "images.yaml":
				serviceCount += availableSvcImage(svcData)
			case "flavors.yaml":
				serviceCount += availableSvcInstanceTypes(svcData)
			default:
				continue
			}
			logger.Info("Counted services from ConfigMap", "ConfigMap", cm.Name, "Svc", svc, "Count", serviceCount)
		}
	}

	return serviceCount, nil
}

func decodeSvcYaml(yamlData string) ([]map[string]any, error) {
	var services []map[string]any
	if err := yaml.Unmarshal([]byte(yamlData), &services); err != nil {
		return nil, fmt.Errorf("failed to decode YAML: %w", err)
	}
	return services, nil
}

func availableSvcImage(imgs []map[string]any) int {
	if len(imgs) == 0 {
		return 0 // Return 0 if no images found
	}
	imgNames := lo.Map(imgs, func(img map[string]any, _ int) string {
		z, ok := img["zone"].(string)
		n, ok2 := img["name"].(string)
		if !ok || !ok2 || z == "" || n == "" {
			return "" // Skip if zone or name is not a string
		}
		return n
	})

	// Count the number of services in the images.yaml
	return len(imgNames)
}

func availableSvcInstanceTypes(s []map[string]any) int {
	count := 0
	// Assuming each service in the slice has a "zone" key to indicate zonal services
	if len(s) == 0 {
		return count
	}
	// Iterate through the slice and count services with "zone" key
	zonalSvc := lo.Reduce(s,
		func(acc map[string][]string, svc map[string]any, _ int) map[string][]string {
			zone, _ := svc["zone"].(string)
			if zone == "" {
				return acc
			}
			flavors, ok := svc["flavors"].([]any)
			if !ok || len(flavors) == 0 {
				return acc
			}

			// extract non-empty flavor names
			names := make([]string, 0, len(flavors))
			for _, f := range flavors {
				if m, ok := f.(map[string]any); ok {
					if name, _ := m["name"].(string); name != "" {
						names = append(names, name)
					}
				}
			}
			if len(names) > 0 {
				acc[zone] = names
			}
			return acc
		}, map[string][]string{})

	// Count the number of zonal services
	for _, flavors := range zonalSvc {
		if len(flavors) > 0 {
			count += len(flavors)
		}
	}
	return count
}
