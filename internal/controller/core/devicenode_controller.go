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
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"

	"gopkg.in/yaml.v3"
)

// DeviceNodeReconciler reconciles a DeviceNode object
type DeviceNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
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
	pZones := lo.Map(provider.Spec.Zones, func(z cv1a1.ZoneSpec, _ int) string { return z.Name })
	if !lo.Contains(pZones, strings.ToLower(dn.Spec.DeviceSpec.Zone)) {
		return ctrl.Result{}, nil
	}

	dn.Status.Region = provider.Spec.Region
	dn.Status.Zone = dn.Spec.DeviceSpec.Zone

	if dn.Spec.DeviceSpec != nil {
		err := r.updateConfigMap(ctx, provider, req, dn.Spec.DeviceSpec)
		if err != nil {
			r.Logger.Error(err, "failed to update ConfigMap for images")
			dn.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "ConfigMapError", err.Error())
			// best effort to update status
			_ = r.Status().Update(ctx, dn)
			// Requeue for retry
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, err
		}
	}

	dn.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "Reconciled", "DeviceNode reconciled successfully")

	// Update the DeviceNode status
	if err := r.Status().Update(ctx, dn); err != nil {
		if apierrors.IsConflict(err) {
			r.Logger.Info("Conflict when updating DeviceNode status, requeuing", "name", req.Name)
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		} else {
			r.Logger.Error(err, "failed to update DeviceNode status")
			return ctrl.Result{}, err
		}
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

func (r *DeviceNodeReconciler) updateConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile, req ctrl.Request, dnSpec *cv1a1.DeviceZoneSpec) error {
	// early return if both are nil
	if dnSpec == nil {
		return nil
	}
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name

	cmList := &corev1.ConfigMapList{}
	if err := r.List(ctx, cmList, client.MatchingLabels(ll), client.InNamespace(hint.SKYCLUSTER_NAMESPACE)); err != nil {
		return fmt.Errorf("unable to list ConfigMaps for images: %w", err)
	}
	if len(cmList.Items) != 1 {
		return fmt.Errorf("error listing ConfigMaps for images: expected 1, got %d", len(cmList.Items))
	}
	cm := cmList.Items[0]

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	_, err := r.createOrUpdateConfigMapData(ctx, req, &cm, dnSpec)
	if err != nil {
		return fmt.Errorf("failed to create or update ConfigMap data: %w", err)
	}

	return nil
}

func (r *DeviceNodeReconciler) createOrUpdateConfigMapData(ctx context.Context, req ctrl.Request, cm *corev1.ConfigMap, dnSpec *cv1a1.DeviceZoneSpec) (bool, error) {

	if dnSpec == nil || dnSpec.Type == "" {
		return false, fmt.Errorf("invalid DeviceZoneSpec")
	}

	deviceType := dnSpec.Type

	// if there is no data for this device type, initialize it
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	// Use a typed map to avoid dumping type metadata into YAML
	dt := make(map[string]cv1a1.DeviceZoneSpec)

	if data, exists := cm.Data[deviceType]; exists && data != "" {
		if err := yaml.Unmarshal([]byte(data), &dt); err != nil {
			// corrupted entry: reset to empty map instead of failing reconciliation
			r.Logger.Info("corrupted ConfigMap entry, resetting", "deviceType", deviceType, "error", err)
			dt = make(map[string]cv1a1.DeviceZoneSpec)
		}
	}

	// If identical, no update needed
	if old, exists := dt[req.Name]; exists && reflect.DeepEqual(old, *dnSpec) {
		return false, nil
	}

	// Set/replace entry
	dt[req.Name] = *dnSpec

	out, err := yaml.Marshal(dt)
	if err != nil {
		return false, err
	}

	cm.Data[deviceType] = string(out)
	if err := r.Update(ctx, cm); err != nil {
		return false, fmt.Errorf("failed to update ConfigMap for device type %s: %w", deviceType, err)
	}

	return true, nil
}
