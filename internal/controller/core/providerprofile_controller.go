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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// "github.com/aws/aws-sdk-go-v2/aws"
	// "github.com/aws/aws-sdk-go-v2/config"
	// "github.com/aws/aws-sdk-go-v2/credentials"
	// ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	// ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	// pricing "github.com/aws/aws-sdk-go-v2/service/pricing"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	helper "github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

// ProviderProfileReconciler reconciles a ProviderProfile object
type ProviderProfileReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/finalizers,verbs=update

func (r *ProviderProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(helper.CustomLogger()).WithName("[ProviderProfile]")
	logger.Info(fmt.Sprintf("Reconciler started for %s", req.Name))

	// Copy all values from spec to status
	pf := &corev1alpha1.ProviderProfile{}
	if err := r.Get(ctx, req.NamespacedName, pf); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to fetch ProviderProfile")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		logger.Info("ProviderProfile not found, maybe deleted", "name", req.Name)
		if err := r.cleanUp(ctx, pf); err != nil {
			logger.Error(err, "cleanup error", "name", req.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Add labels based on the provider spec
	if pf.Labels == nil {
		pf.Labels = make(map[string]string)
	}
	pf.Labels["skycluster.io/provider-platform"] = pf.Spec.Platform
	pf.Labels["skycluster.io/provider-region"] = pf.Spec.Region

	// Update the provider with the new labels
	if err := r.Update(ctx, pf); err != nil {
		logger.Error(err, "unable to update ProviderProfile labels")
		return ctrl.Result{}, err
	}

	// Copy all spec values to status
	pf.Status.Enabled = pf.Spec.Enabled
	pf.Status.Zones = make([]corev1alpha1.ZoneSpec, len(pf.Spec.Zones))
	pf.Status.Zones = lo.Filter(pf.Spec.Zones, func(zone corev1alpha1.ZoneSpec, _ int) bool { return true })
	if err := r.Status().Update(ctx, pf); err != nil {
		logger.Error(err, "unable to update ProviderProfile status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.ProviderProfile{}).
		Named("core-providerprofile").
		Complete(r)
}

func (r *ProviderProfileReconciler) cleanUp(ctx context.Context, pf *corev1alpha1.ProviderProfile) error {

	// Clean up ConfigMaps related to the provider profile
	p := pf.Spec.Platform
	rg := pf.Spec.Region
	defaultZone, ok := lo.Find(pf.Spec.Zones, func(zone corev1alpha1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	if !ok {
		return fmt.Errorf("no default zone found for provider profile %s", pf.Name)
	}
	cm := &corev1.ConfigMapList{}
	// List all ConfigMaps in the namespace with matching labels
	if err := r.List(ctx, cm, &client.ListOptions{
		Namespace: helper.SKYCLUSTER_NAMESPACE,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"skycluster.io/provider-platform": p,
			"skycluster.io/provider-region":   rg,
			"skycluster.io/provider-zone":     defaultZone.Name,
		}),
	}); err != nil {
		return fmt.Errorf("unable to list ConfigMaps for cleanup: %w", err)
	}
	if len(cm.Items) > 1 {
		return fmt.Errorf("multiple ConfigMaps found for provider profile %s, expected only one", pf.Name)
	}
	// Delete each ConfigMap found
	for _, cmItem := range cm.Items {
		if err := r.Delete(ctx, &cmItem); err != nil {
			return fmt.Errorf("unable to delete ConfigMap %s during cleanup: %w", cmItem.Name, err)
		}
	}

	return nil
}
