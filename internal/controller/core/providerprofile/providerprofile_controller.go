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

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller/utils"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"
	pkgenc "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/encoding"
)

// ProviderProfileReconciler reconciles a ProviderProfile object
type ProviderProfileReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Logger   logr.Logger
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=latencies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/finalizers,verbs=update

/*

- On spec change or ResyncRequired, the reconciler ensures a ConfigMap and (for aws/azure/gcp) Image and InstanceType resources exist, then records them in pf.Status.DependencyManager with SetDependency(name, kind, namespace).
- It sets pf.Status.ObservedGeneration and marks Ready=false ("FreshReconcile") after creating/ensuring dependencies, then adds a finalizer (hint.FN_Dependency) and requeues.
- The finalizer protects cleanup: on deletion, if the finalizer is present the reconciler calls cleanUp (best-effort cleanup of managed resources like ConfigMaps), removes the finalizer, and updates the object so deletion can complete.
- If no spec changes, the reconciler populates static status fields (Platform/Region/Zones) and delegates to platform-specific handlers (cloud platforms → handlerPlatformCloud; baremetal → handlerPlatformBareMetal) which may further observe or reconcile dependencies.
- ResyncRequired condition is cleared when handled; Ready is set False for fresh or unhandled/custom platforms. Reconciliation always requeues periodically (RequeueAfter hint).
*/

/*
Reconcile behavior:

- Being deleted: clean up and return
- No changes and no ResyncRequired condition: return
- Spec changes OR ResyncRequired condition:
	- Ensure ConfigMap

	- set status

  - Major clouds:
		- Ensure Image, InstanceType if they don't exist (major clouds only)
		- [ResyncRequired Y] if dependencies not found (major cloud only)

	- Update status: [observedGeneration]
	- Requeue  [Ready false]
- No changes: poll data
  - not ready? requeue [Ready false]
	- does not exist? requeue [Ready N, ResyncRequired Y]
	- ready? update and return
*/

func (r *ProviderProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started", "name", req.Name, "namespace", req.Namespace)

	// Copy all values from spec to status
	pf := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, req.NamespacedName, pf); err != nil {
		r.Logger.Info("unable to fetch ProviderProfile, may be deleted", "name", req.Name, "ProviderProfile", pf.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	specChanged := pf.Generation != pf.Status.ObservedGeneration

	// If object is being deleted
	if !pf.DeletionTimestamp.IsZero() {
		// If finalizer is present, clean up ConfigMaps
		r.Logger.Info("ProviderProfile is being deleted", "name", req.Name, "ProviderProfile", pf.Name)
		if controllerutil.ContainsFinalizer(pf, hint.FN_Dependency) {
			r.Logger.Info("ProviderProfile has finalizer, cleaning up resources", "name", req.Name, "ProviderProfile", pf.Name)
			// Best effort Clean up resources
			_ = r.cleanUp(ctx, pf)
			// Remove finalizer once cleanup is done
			_ = controllerutil.RemoveFinalizer(pf, hint.FN_Dependency)
			if err := r.Update(ctx, pf); err != nil {
				r.Logger.Error(err, "unable to remove finalizer from ProviderProfile")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	resyncRequired := meta.IsStatusConditionTrue(pf.Status.Conditions, string(hv1a1.ResyncRequired))
	if specChanged || resyncRequired {
		r.Logger.Info("spec changed or resync required, ensuring ConfigMap and dependencies", "ProviderProfile", pf.Name)
		// Create a ConfigMap if it doesn't exist
		cm, err := r.ensureConfigMap(ctx, pf)
		if err != nil {
			r.Logger.Error(err, "unable to ensure ConfigMap for ProviderProfile")
			return ctrl.Result{}, err
		}
		pf.Status.DependencyManager.SetDependency(cm.Name, "ConfigMap", pf.Namespace)

		// if resync is required, set it to false
		if resyncRequired {
			pf.Status.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "NoResyncNeeded", "Resync not needed, waiting for dependencies to be ready")
		}

		// Platform is one of the major clouds? then ensure dependencies:
		if lo.Contains([]string{"aws", "azure", "gcp"}, strings.ToLower(pf.Spec.Platform)) {
			img, err := r.ensureImages(ctx, pf)
			if err != nil {
				r.Logger.Error(err, "unable to ensure Images for ProviderProfile")
				return ctrl.Result{}, err
			}
			instanceType, err := r.ensureInstanceTypes(ctx, pf)
			if err != nil {
				r.Logger.Error(err, "unable to ensure InstanceTypes for ProviderProfile")
				return ctrl.Result{}, err
			}
			pf.Status.DependencyManager.SetDependency(img.Name, "Image", pf.Namespace)
			pf.Status.DependencyManager.SetDependency(instanceType.Name, "InstanceType", pf.Namespace)
		}

		// ensure latencies
		err = r.ensureLatencies(ctx, pf)
		if err != nil {
			r.Logger.Error(err, "unable to ensure Latencies for ProviderProfile")
			return ctrl.Result{}, err
		}

		err = r.ensureEgressCosts(pf)
		if err != nil {
			r.Logger.Error(err, "unable to ensure EgressCosts for ProviderProfile")
			return ctrl.Result{}, err
		}

		pf.Status.ObservedGeneration = pf.Generation
		pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "FreshReconcile", "Fresh reconcile: ConfigMap and dependencies ensured")
		r.Logger.Info("ProviderProfile spec changed, ConfigMap and dependencies ensured", "name", req.Name, "ProviderProfile", pf.Name)
		_ = r.Status().Update(ctx, pf)

		// ensure finalizer if not present
		if !controllerutil.ContainsFinalizer(pf, hint.FN_Dependency) {
			r.Logger.Info("Adding finalizer to ProviderProfile", "name", req.Name, "ProviderProfile", pf.Name)
			controllerutil.AddFinalizer(pf, hint.FN_Dependency)
		}
		if err := r.Update(ctx, pf); err != nil {
			r.Logger.Error(err, "Failed to update ProviderProfile with finalizer")
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// no spec changes, poll data, set static status
	pf.Status.Platform = pf.Spec.Platform
	pf.Status.Region = pf.Spec.Region
	pf.Status.Zones = pf.Spec.Zones

	switch strings.ToLower(pf.Spec.Platform) {
	case "aws", "azure", "gcp", "openstack":
		return r.handlerPlatformCloud(ctx, pf, req)
	case "baremetal":
		return r.handlerPlatformBareMetal(ctx, pf, req)
	default:
		r.Logger.Info("Custom platform detected, no specific handler implemented", "platform", pf.Spec.Platform)
		pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "CustomPlatform", "Custom platform detected, no specific handler implemented")
		// r.handlerPlatformCustom(pf) // not implemented
	}

	_ = r.Status().Update(ctx, pf)
	return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.ProviderProfile{}).
		Named("core-providerprofile").
		Complete(r)
}

func (r *ProviderProfileReconciler) handlerPlatformCloud(ctx context.Context, pf *cv1a1.ProviderProfile, req ctrl.Request) (ctrl.Result, error) {
	// fetch latest image and instance type data (if "ready") and update configmap
	img, err1 := r.fetchImageData(ctx, pf)
	if err1 != nil {
		msg := fmt.Sprintf("Failed to fetch Image data for ProviderProfile %s: %v", pf.Name, err1)
		if !apierrors.IsNotFound(err1) {
			r.Logger.Error(err1, msg)
		}
	}

	it, err2 := r.fetchInstanceTypeData(ctx, pf)
	if err2 != nil {
		msg := fmt.Sprintf("Failed to fetch InstanceType data for ProviderProfile %s: %v", pf.Name, err2)
		if !apierrors.IsNotFound(err2) {
			r.Logger.Error(err2, msg)
		}
	}

	// Update the CM with the latest data
	// TODO: this function return error on configmap update failure
	err3 := r.updateConfigMap(ctx, pf, img, it)

	if err1 != nil || err2 != nil || err3 != nil {
		msg := "Failed to fetch dependencies, [ResynceRequired]"
		r.Logger.Info(msg, "name", req.Name, "ProviderProfile", pf.Name)
		pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "DependencyFetchUpdateFailed", msg)
		// if this is a custom provider, we may check if dependencies are available
		if !lo.Contains([]string{"aws", "azure", "gcp"}, strings.ToLower(pf.Spec.Platform)) {
			pf.Status.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "NoResyncNeeded", "Resync not needed, waiting for dependencies to be ready")
		} else {
			pf.Status.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "DependencyFetchUpdateFailed", msg)
		}

		if err := r.Status().Update(ctx, pf); err != nil {
			r.Logger.Error(err, "unable to update ProviderProfile status after data fetch failure")
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// If no error and dep objects are ready, update the status
	imgReady := meta.IsStatusConditionTrue(img.Status.Conditions, string(hv1a1.Ready))
	instanceTypeReady := meta.IsStatusConditionTrue(it.Status.Conditions, string(hv1a1.Ready))

	if imgReady && instanceTypeReady {
		pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "Ready", "Image and InstanceType are ready")
		pf.Status.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "NoResyncNeeded", "No resync needed, dependencies are ready")

		if err := r.Status().Update(ctx, pf); err != nil {
			if apierrors.IsConflict(err) {
				r.Logger.Info("Conflict while updating ProviderProfile status, requeuing", "name", req.Name)
				return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
			}
			r.Logger.Error(err, "unable to update ProviderProfile status")
			return ctrl.Result{}, err
		}

		// We are good for now, request a requeue after a longer period
		return ctrl.Result{RequeueAfter: 12 * time.Hour}, nil
	} else {
		r.Logger.Info("Image or InstanceType not ready, requeuing", "name", req.Name, "ProviderProfile", pf.Name)
	}

	pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "DependenciesNotReady", "Image or InstanceType not ready, requeuing")
	_ = r.Status().Update(ctx, pf)
	return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
}

// handlerPlatformBareMetal creates a ConfigMap and set status as ready.
func (r *ProviderProfileReconciler) handlerPlatformBareMetal(ctx context.Context, pf *cv1a1.ProviderProfile, req ctrl.Request) (ctrl.Result, error) {
	if err := r.updateConfigMap(ctx, pf, nil, nil); err != nil {

		msg := fmt.Sprintf("Failed to update ConfigMap for ProviderProfile %s: %v", pf.Name, err)
		r.Logger.Error(err, msg)
		pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "ConfigMapUpdateFailed", msg)
		_ = r.Status().Update(ctx, pf)

		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil

	} else {

		pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "Ready", "BareMetal platform, configmap configured.")
		_ = r.Status().Update(ctx, pf)
		// we are good for now, request a requeue after a longer period
		return ctrl.Result{RequeueAfter: 12 * time.Hour}, nil

	}
}

// create or update
func (r *ProviderProfileReconciler) ensureInstanceTypes(ctx context.Context, pf *cv1a1.ProviderProfile) (*cv1a1.InstanceType, error) {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name

	its := &cv1a1.InstanceTypeList{}
	if err := r.List(ctx, its, &client.ListOptions{
		Namespace:     pf.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch InstanceTypes for ProviderProfile %s: %w", pf.Name, err)
	}

	it := &cv1a1.InstanceType{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    pf.Namespace,
			GenerateName: fmt.Sprintf("%s-", pf.Name),
			Labels:       labels.Set(ll),
		},
		Spec: cv1a1.InstanceTypeSpec{
			ProviderRef: pf.Name,
			Offerings:   getTypeFamilies(pf.Spec.Platform, pf.Spec.Zones),
		},
	}

	// If InstanceTypes already exist, check if update is needed
	if len(its.Items) > 0 && reflect.DeepEqual(its.Items[0].Spec, it.Spec) {
		return &its.Items[0], nil // Return the existing InstanceTypes
	}

	// Set the owner reference to the ProviderProfile
	if err := controllerutil.SetControllerReference(pf, it, r.Scheme); err != nil {
		return nil, fmt.Errorf("unable to set owner reference for InstanceTypes: %w", err)
	}

	// Create the InstanceTypes
	if err := r.Create(ctx, it); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("unable to create InstanceTypes: %w", err)
	}
	return it, nil
}

func makeLatencyName(a, b string) string {
	// sanitize names to be DNS-1123 compatible: lowercase, replace invalids with '-'
	return "latency-" + utils.SanitizeName(a) + "-" + utils.SanitizeName(b)
}

// Set default values for EgressCostSpec Tiers if not provided
func (r *ProviderProfileReconciler) ensureEgressCosts(pf *cv1a1.ProviderProfile) error {
	// Create EgressCost objects for this provider if not exists
	pf.Status.EgressCostSpecs = []cv1a1.EgressCostSpec{}
	for _, cost := range pf.Spec.EgressCosts {
		pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
			Type:        cost.Type,
			Destination: cost.Destination,
			Unit:        cost.Unit,
			Tiers:       cost.Tiers,
		})
	}

	if len(pf.Status.EgressCostSpecs) == 0 {
		// set default egress costs based on platform
		plt := pf.Spec.Platform
		// set costs for public clouds only
		switch strings.ToLower(plt) {
		case "aws":
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "internet",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.09"}},
			})
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "inter-zone",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.01"}},
			})
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "inter-region",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.02"}},
			})
		case "azure":
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "internet",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.087"}},
			})
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "inter-zone",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.01"}},
			})
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "inter-region",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.02"}},
			})
		case "gcp":
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "internet",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 1000, PricePerGB: "0.12"}},
			})
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "inter-zone",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.01"}},
			})
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "inter-region",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 10000, PricePerGB: "0.02"}},
			})
		default:
			pf.Status.EgressCostSpecs = append(pf.Status.EgressCostSpecs, cv1a1.EgressCostSpec{
				Type:  "internet",
				Unit:  "GB",
				Tiers: []cv1a1.EgressTier{{FromGB: 0, ToGB: 1000, PricePerGB: "0.0"}},
			})
		}
	}
	return nil
}

func (r *ProviderProfileReconciler) ensureLatencies(ctx context.Context, pf *cv1a1.ProviderProfile) error {
	// Create Latency objects between this provider and all other providers if not exists
	var provList cv1a1.ProviderProfileList
	if err := r.List(ctx, &provList, client.InNamespace(pf.Namespace)); err != nil {
		return err
	}
	for _, other := range provList.Items {
		if other.Name == pf.Name {
			continue
		}
		aName := pf.Spec.Platform + "-" + pf.Spec.Region
		bName := other.Spec.Platform + "-" + other.Spec.Region
		a, b := utils.CanonicalPair(aName, bName)
		latName := makeLatencyName(a, b)
		var lat cv1a1.Latency
		err := r.Get(ctx, types.NamespacedName{Namespace: pf.Namespace, Name: latName}, &lat)
		if errors.IsNotFound(err) {
			// create
			z, ok1 := lo.Find(pf.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
			o, ok2 := lo.Find(other.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
			if !ok1 || !ok2 {
				continue
			}
			fixedMs, err := utils.GenerateSyntheticLatency(
				pf.Spec.Region, other.Spec.Region, pf.Spec.RegionAlias, other.Spec.RegionAlias, z.Type, o.Type)
			if err != nil {
				continue
			}
			newLat := cv1a1.Latency{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: pf.Namespace,
					Name:      latName,
					Labels: map[string]string{
						"skycluster.io/provider-pair":     utils.SanitizeName(a) + "-" + utils.SanitizeName(b),
						"skycluster.io/provider-platform": pf.Spec.Platform,
						"skycluster.io/provider-region":   pf.Spec.Region,
					},
				},
				Spec: cv1a1.LatencySpec{
					ProviderRefA:   corev1.LocalObjectReference{Name: a},
					ProviderRefB:   corev1.LocalObjectReference{Name: b},
					FixedLatencyMs: fmt.Sprintf("%.2f", fixedMs),
				},
			}
			if err := r.Create(ctx, &newLat); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else { // exists - optionally ensure; update
			// z, ok1 := lo.Find(pf.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
			// o, ok2 := lo.Find(other.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
			// if !ok1 || !ok2 {
			// 	continue
			// }
			// fixedMs, err := utils.GenerateSyntheticLatency(
			// 	pf.Spec.Region, other.Spec.Region, pf.Spec.RegionAlias, other.Spec.RegionAlias, z.Type, o.Type)
			// if err != nil {
			// 	continue
			// }
			// lat.Spec.FixedLatencyMs = fmt.Sprintf("%.2f", fixedMs)
			// _ = r.Update(ctx, &lat)
			_ = lat
		}
	}
	return nil
}

// create or update
func (r *ProviderProfileReconciler) ensureImages(ctx context.Context, pf *cv1a1.ProviderProfile) (*cv1a1.Image, error) {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name

	imgs := &cv1a1.ImageList{}
	if err := r.List(ctx, imgs, &client.ListOptions{
		Namespace:     pf.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch Images for ProviderProfile %s: %w", pf.Name, err)
	}
	// if images already exist, check if update is needed

	img := &cv1a1.Image{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    pf.Namespace,
			GenerateName: fmt.Sprintf("%s-", pf.Name),
			Labels:       labels.Set(ll),
		},
		Spec: cv1a1.ImageSpec{
			ProviderRef: pf.Name,
			Images: []cv1a1.ImageOffering{
				{
					NameLabel: "ubuntu-20.04",
					Pattern:   "*hvm-ssd*/ubuntu-focal-20.04-amd64-server*",
				},
				{
					NameLabel: "ubuntu-22.04",
					Pattern:   "*hvm-ssd*/ubuntu-jammy-22.04-amd64-server*",
				},
				{
					NameLabel: "ubuntu-24.04",
					Pattern:   "*hvm-ssd*/ubuntu-noble-24.04-amd64-server*",
				},
				{
					NameLabel: "ubuntu-24.04-gpu",
					Pattern:   "*skypilot-aws-gpu-ubuntu-241104*",
				},
			},
		},
	}

	if len(imgs.Items) > 0 && reflect.DeepEqual(imgs.Items[0].Spec, img.Spec) {
		return &imgs.Items[0], nil // Return the existing Images
	}

	// Set the owner reference to the ProviderProfile
	if err := controllerutil.SetControllerReference(pf, img, r.Scheme); err != nil {
		return nil, fmt.Errorf("unable to set owner reference for Images: %w", err)
	}

	// Create the Images
	if err := r.Create(ctx, img); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("unable to create Images: %w", err)
	}
	return img, nil
}

func (r *ProviderProfileReconciler) ensureConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile) (*corev1.ConfigMap, error) {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name
	ll["skycluster.io/config-type"] = "provider-profile"

	cms := &corev1.ConfigMapList{}
	if err := r.List(ctx, cms, &client.ListOptions{
		Namespace:     hint.SKYCLUSTER_NAMESPACE,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch ConfigMap for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(cms.Items) > 0 {
		return &cms.Items[0], nil // Return the first ConfigMap found
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    hint.SKYCLUSTER_NAMESPACE,
			GenerateName: fmt.Sprintf("%s-", pf.Name),
			Labels:       labels.Set(ll),
		},
	}

	// Set the owner reference to the ProviderProfile
	// does not work since ConfigMap is in a different namespace

	// Create the ConfigMap
	if err := r.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("unable to create ConfigMap: %w", err)
	}

	return cm, nil
}

func (r *ProviderProfileReconciler) cleanUpLatencyData(ctx context.Context, pf *cv1a1.ProviderProfile) error {
	// Get all providerprofiles objects and build the labels
	var labels []string
	var provList cv1a1.ProviderProfileList
	if err := r.List(ctx, &provList, client.InNamespace(pf.Namespace)); err != nil {
		return err
	}
	for _, other := range provList.Items {
		if other.Name == pf.Name {
			continue
		}
		a, b := utils.CanonicalPair(pf.Name, other.Name)
		latName := utils.SanitizeName(a) + "-" + utils.SanitizeName(b)
		labels = append(labels, latName)
	}

	sel, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      "skycluster.io/provider-pair",
			Operator: metav1.LabelSelectorOpIn,
			Values:   labels,
		}},
	})
	if err != nil {
		return err
	}

	if err := r.DeleteAllOf(ctx, &cv1a1.Latency{},
		client.InNamespace(pf.Namespace),
		client.MatchingLabelsSelector{Selector: sel},
	); err != nil {
		return err
	}

	return nil
}

func (r *ProviderProfileReconciler) cleanUpInstanceTypes(ctx context.Context, pf *cv1a1.ProviderProfile) error {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "instance-types")
	its := &cv1a1.InstanceTypeList{}
	if err := r.List(ctx, its, &client.ListOptions{
		Namespace:     pf.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return fmt.Errorf("unable to fetch InstanceTypes for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(its.Items) == 0 {
		return nil
	}

	for _, itItem := range its.Items {
		if err := r.Delete(ctx, &itItem); err != nil {
			return fmt.Errorf("unable to delete InstanceTypes %s during cleanup: %w", itItem.Name, err)
		}
	}
	return nil
}

func (r *ProviderProfileReconciler) cleanUpImages(ctx context.Context, pf *cv1a1.ProviderProfile) error {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "images")

	imgs := &cv1a1.ImageList{}
	if err := r.List(ctx, imgs, &client.ListOptions{
		Namespace:     pf.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return fmt.Errorf("unable to fetch Images for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(imgs.Items) == 0 {
		return nil
	}

	for _, imgItem := range imgs.Items {
		if err := r.Delete(ctx, &imgItem); err != nil {
			return fmt.Errorf("unable to delete Images %s during cleanup: %w", imgItem.Name, err)
		}
	}
	return nil
}

func (r *ProviderProfileReconciler) cleanUpConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile) error {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name
	ll["skycluster.io/config-type"] = "provider-profile"

	// Get the ConfigMap associated with the provider profile
	cms := &corev1.ConfigMapList{}
	if err := r.List(ctx, cms, &client.ListOptions{
		Namespace:     hint.SKYCLUSTER_NAMESPACE,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return fmt.Errorf("unable to fetch ConfigMap for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(cms.Items) == 0 {
		return nil // No ConfigMap to clean up
	}

	// if no error, then there is a ConfigMap to clean up
	for _, cmItem := range cms.Items {
		if err := r.Delete(ctx, &cmItem); err != nil {
			return fmt.Errorf("unable to delete ConfigMap %s during cleanup: %w", cmItem.Name, err)
		}
	}
	return nil
}

func (r *ProviderProfileReconciler) cleanUp(ctx context.Context, pf *cv1a1.ProviderProfile) error {
	err1 := r.cleanUpConfigMap(ctx, pf)
	err2 := r.cleanUpImages(ctx, pf)
	err3 := r.cleanUpInstanceTypes(ctx, pf)
	err4 := r.cleanUpLatencyData(ctx, pf)

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return fmt.Errorf("failed to clean up resources for ProviderProfile %s, %v", pf.Name, err4)
	}

	return nil
}

// If any of the input data is not nil, it will update the ConfigMap with the data.
// If both are nil, it will not update the ConfigMap and return nil.
func (r *ProviderProfileReconciler) updateConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile, img *cv1a1.Image, it *cv1a1.InstanceType) error {
	// early return if both are nil
	if img == nil && it == nil {
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

	updated := false
	if img != nil {
		imgYamlData, err1 := pkgenc.EncodeObjectToYAML(img.Status.Images)
		if err1 == nil {
			cm.Data["images.yaml"] = imgYamlData
			updated = true
		}
	}

	if it != nil {
		itYamlData, err2 := pkgenc.EncodeObjectToYAML(it.Status.Offerings)
		if err2 == nil {
			cm.Data["flavors.yaml"] = itYamlData
			updated = true
		}
	}

	// add managed cluster (EKS/GKE/AKS generic mapping) service and costs
	// fixed cost values for common managed control plane offerings and
	// an estimated overhead cost for a "system" node type (hosting e.g. karpenter).
	// for cloud providers only
	if lo.Contains([]string{"aws", "azure", "gcp"}, strings.ToLower(pf.Spec.Platform)) {
		managedClusters := []map[string]any{
			{
				"nameLabel": "ManagedKubernetes",
				"name":      lo.If(pf.Spec.Platform == "aws", "EKS").ElseIf(pf.Spec.Platform == "gcp", "GKE").Else("AKS"),
				"price":     lo.If(pf.Spec.Platform == "aws", "0.10").ElseIf(pf.Spec.Platform == "gcp", "0.15").Else("0.10"),
				"overhead": map[string]any{
					"instanceType": lo.If(pf.Spec.Platform == "aws", "m5.xlarge").ElseIf(pf.Spec.Platform == "gcp", "e2-standard-2").Else("Standard_D2s_v3"),
					"cost":         lo.If(pf.Spec.Platform == "aws", "0.096").ElseIf(pf.Spec.Platform == "gcp", "0.067").Else("0.096"),
					"count":        1,
				},
			},
		}
		managedYaml, err3 := pkgenc.EncodeObjectToYAML(managedClusters)
		if err3 == nil {
			cm.Data["managed-k8s.yaml"] = managedYaml
			updated = true
		}
	}

	if updated {
		if err := r.Update(ctx, &cm); err != nil {
			return fmt.Errorf("failed to update ConfigMap for images: %w", err)
		}
	}
	return nil
}

// fetchImageData fetches the Image data for the given ProviderProfile.
// It returns nil if no image is found or if the Image is not ready.
// and error if any issue occurs during the fetch.
func (r *ProviderProfileReconciler) fetchImageData(ctx context.Context, pf *cv1a1.ProviderProfile) (*cv1a1.Image, error) {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name

	imgList := &cv1a1.ImageList{}
	if err := r.List(ctx, imgList, &client.ListOptions{
		Namespace:     pf.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch Images for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(imgList.Items) == 0 {
		return nil, errors.NewNotFound(
			schema.GroupResource{Group: "core.skycluster.io", Resource: "images"},
			fmt.Sprintf("no Image found for ProviderProfile %s", pf.Name),
		)
	}

	img := &imgList.Items[0]
	return img, nil
}

// returns nil if no image is found or if the Image is not ready.
// and error if any issue occurs during the fetch.
func (r *ProviderProfileReconciler) fetchInstanceTypeData(ctx context.Context, pf *cv1a1.ProviderProfile) (*cv1a1.InstanceType, error) {
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name

	itList := &cv1a1.InstanceTypeList{}
	if err := r.List(ctx, itList, &client.ListOptions{
		Namespace:     pf.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set(ll)),
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch InstanceTypes for ProviderProfile %s: %w", pf.Name, err)
	}
	if len(itList.Items) == 0 {
		return nil, errors.NewNotFound(
			schema.GroupResource{Group: "core.skycluster.io", Resource: "instancetypes"},
			fmt.Sprintf("no InstanceType found for ProviderProfile %s", pf.Name),
		)
	}

	it := &itList.Items[0]
	return it, nil
}

// func getNumberOfServices(cm *corev1.ConfigMap) (int, error) {
// 	// Assuming each ConfigMap contains a list of services in a specific key
// 	r.Logger.Info("Counting services from ConfigMap", "ConfigMap", cm.Name)
// 	serviceCount := 0

// 	for svc, yamlData := range cm.Data { // svcData["images.yaml"]
// 		r.Logger.Info("Decoding YAML data from ConfigMap", "ConfigMap", cm.Name)
// 		svcData, err := decodeSvcYaml(yamlData)
// 		if err != nil {
// 			return 0, fmt.Errorf("failed to decode YAML in ConfigMap %s: %w", cm.Name, err)
// 		}
// 		r.Logger.Info("Decoded service data from ConfigMap", "ConfigMap", cm.Name, "Service", svc, "Data", len(svcData))
// 		switch svc {
// 		case "images.yaml":
// 			serviceCount += availableSvcImage(svcData)
// 		case "flavors.yaml":
// 			serviceCount += availableSvcInstanceTypes(svcData)
// 		default:
// 			continue
// 		}
// 		r.Logger.Info("Counted services from ConfigMap", "ConfigMap", cm.Name, "Svc", svc, "Count", serviceCount)
// 	}

// 	return serviceCount, nil
// }

func buildZoneOfferings(zoneName string, offerings []string) cv1a1.ZoneOfferings {
	o := make([]hv1a1.InstanceOffering, 0, len(offerings))
	for _, offering := range offerings {
		o = append(o, hv1a1.InstanceOffering{NameLabel: offering})
	}
	return cv1a1.ZoneOfferings{Zone: zoneName, Offerings: o}
}

func getTypeFamilies(platform string, zones []cv1a1.ZoneSpec) []cv1a1.ZoneOfferings {
	zoneNames := lo.Map(zones, func(z cv1a1.ZoneSpec, _ int) string { return z.Name })
	zoneOfferings := make([]cv1a1.ZoneOfferings, 0, len(zoneNames))

	switch platform {
	case "aws":
		for _, z := range zoneNames {
			zo := buildZoneOfferings(z, []string{"t3.", "m5.", "p3.", "p5.", "g5.", "g6.", "g6e.", "p4d", "p3dn"})
			zoneOfferings = append(zoneOfferings, zo)
		}
		// "m5", "m6g", "c5", "c6g", "r5", "r6g"
	case "azure":
		for _, z := range zoneNames {
			zo := buildZoneOfferings(z, []string{"Standard_A", "Standard_B"})
			zoneOfferings = append(zoneOfferings, zo)
		}
	case "gcp":
		for _, z := range zoneNames {
			zo := buildZoneOfferings(z, []string{"e2", "g2", "a2"})
			zoneOfferings = append(zoneOfferings, zo)
		}
	default:
		// return []string{}
		return nil
	}

	return zoneOfferings
}
