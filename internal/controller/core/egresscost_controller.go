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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

// EgressCostReconciler reconciles a EgressCost object
type EgressCostReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=egresscosts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=egresscosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=egresscosts/finalizers,verbs=update

func (r *EgressCostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(pkglog.CustomLogger()).WithName("[EgressCost]")
	logger.Info("Reconciler started.", "name", req.Name)

	// The controller first calculates the egress costs based on the provider specifications.
	// Then if consider user defined inputs from spec, it will override the calculated costs.
	egressCost := &cv1a1.EgressCost{}
	if err := r.Get(ctx, req.NamespacedName, egressCost); err != nil {
		logger.Info("unable to fetch EgressCost")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Load base costs ConfigMap
	var cm corev1.ConfigMap
	cmFound := false
	if err := r.Get(ctx, types.NamespacedName{Name: "provider-egress-costs", Namespace: "skycluster"}, &cm); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ConfigMap costs not found, initializing with defaults")
		} else {
			logger.Error(err, "failed to get base costs ConfigMap")
			return ctrl.Result{}, err
		}
	} else {
		cmFound = true
	}

	var effectiveEgressCosts map[string]map[string]string
	if cmFound && cm.Data["costs"] != "" {
		// Unmarshal existing costs from ConfigMap
		if err := json.Unmarshal([]byte(cm.Data["costs"]), &effectiveEgressCosts); err != nil {
			logger.Error(err, "failed to unmarshal base costs")
			return ctrl.Result{}, err
		}
	} else {
		effectiveEgressCosts = make(map[string]map[string]string)
	}

	// Fetch all providers to calculate costs
	providerList := &cv1a1.ProviderProfileList{}
	if err := r.List(ctx, providerList); err != nil {
		logger.Error(err, "failed to list ProviderProfiles")
		return ctrl.Result{}, err
	}

	// Add any new providers missing from baseCosts with default fallback costs
	for _, p := range providerList.Items {
		if _, exists := effectiveEgressCosts[p.Name]; !exists {
			// Add default costs for new provider, e.g., copy from internet tier or set defaults
			// if _, ok := cv1a1.BaseCosts[p.Spec.Platform]; !ok {
			// 	effectiveEgressCosts[p.Name] = cv1a1.BaseCosts[p.Spec.Platform]
			// } else {
			// 	effectiveEgressCosts[p.Name] = cv1a1.BaseCosts["other"]
			// }
		}
	}

	// Apply user overrides (per source-target-level key)
	if effectiveEgressCosts[egressCost.Spec.ProviderRef] == nil {
		effectiveEgressCosts[egressCost.Spec.ProviderRef] = make(map[string]string)
	}
	effectiveEgressCosts[egressCost.Spec.ProviderRef]["internet"] = egressCost.Spec.ToInternet
	effectiveEgressCosts[egressCost.Spec.ProviderRef]["region"] = egressCost.Spec.ToRegion
	effectiveEgressCosts[egressCost.Spec.ProviderRef]["zone"] = egressCost.Spec.ToZone

	logger.Info("Updated Egress Costs with user overrides")

	// Update the configmap with the effective costs
	effectiveCostsJSON, err := json.Marshal(effectiveEgressCosts)
	if err != nil {
		logger.Error(err, "failed to marshal effective costs")
		return ctrl.Result{}, err
	}

	// if the ConfigMap is not found, create it
	if !cmFound {
		cm.Name = "provider-egress-costs"
		cm.Namespace = "skycluster"
		// Add skycluster labels
		if cm.Labels == nil {
			cm.Labels = make(map[string]string)
		}
		cm.Labels["skycuster.io/managed-by"] = "skycluster"
		cm.Labels["skycuster.io/config-type"] = "egress-costs"
		cm.Data = map[string]string{
			"egress-costs": string(effectiveCostsJSON),
		}
		if err := r.Create(ctx, &cm); err != nil {
			logger.Error(err, "failed to create effective costs ConfigMap")
			return ctrl.Result{}, err
		}
		logger.Info("Created ConfigMap with effective costs", "name", cm.Name)
		return ctrl.Result{}, nil
	}
	// if the ConfigMap is found, update it
	cm.Data["egress-costs"] = string(effectiveCostsJSON)
	if err := r.Update(ctx, &cm); err != nil {
		logger.Error(err, "failed to update effective costs ConfigMap")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciled EgressCost successfully")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EgressCostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.EgressCost{}).
		Named("core-egresscost").
		Complete(r)
}
