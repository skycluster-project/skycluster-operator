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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
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

// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providerprofiles/finalizers,verbs=update

/*
Reconcile behavior:

- Being deleted: clean up and return
- No changes and no ResyncRequired condition: return
- Spec changes OR ResyncRequired condition:
	- Ensure ConfigMap

	- set statis status

  - Major clouds:
		- Ensure Image, InstanceType if they don't exist (major clouds only)
	- Update status: [obsGen]
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
		// Create a ConfigMap if it doesn't exist
		cm, err := r.ensureConfigMap(ctx, pf)
		if err != nil {
			r.Logger.Error(err, "unable to ensure ConfigMap for ProviderProfile")
			return ctrl.Result{}, err
		}
		pf.Status.DependencyManager.SetDependency(cm.Name, "ConfigMap", pf.Namespace)

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
	pf.Status.Region = pf.Spec.Region
	pf.Status.Zones = pf.Spec.Zones

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
		pf.Status.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "DependencyFetchUpdateFailed", msg)
		if err := r.Status().Update(ctx, pf); err != nil {
			r.Logger.Error(err, "unable to update ProviderProfile status after data fetch failure")
			return ctrl.Result{}, err
		}

		// We don't need to requeue if it is not a major cloud provider
		if lo.Contains([]string{"aws", "azure", "gcp"}, strings.ToLower(pf.Spec.Platform)) {
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
		} else { return ctrl.Result{}, nil }

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

	// not ready:
	pf.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "NotReady", "Image or InstanceType is not ready")
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
			ProviderRef:  pf.Name,
			Offerings: getTypeFamilies(pf.Spec.Platform, pf.Spec.Zones),
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
					NameLabel:  "ubuntu-20.04",
				},
				{
					NameLabel:  "ubuntu-22.04",
				},
				{
					NameLabel:  "ubuntu-24.04",
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

	if err1 != nil || err2 != nil || err3 != nil {
		return fmt.Errorf("failed to clean up resources for ProviderProfile %s", pf.Name)
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

	imgYamlData, err1 := pkgenc.EncodeObjectToYAML(img.Status.Images)
	itYamlData, err2 := pkgenc.EncodeObjectToYAML(it.Status.Offerings)

	if err1 == nil {
		cm.Data["images.yaml"] = imgYamlData
	}
	if err2 == nil {
		cm.Data["flavors.yaml"] = itYamlData
	}

	if err1 != nil || err2 != nil {
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
	o := make([]cv1a1.InstanceOffering, 0, len(offerings))
	for _, offering := range offerings {
		o = append(o, cv1a1.InstanceOffering{NameLabel: offering})
	}
	return cv1a1.ZoneOfferings{ Zone: zoneName, Offerings: o }
}

func getTypeFamilies(platform string, zones []cv1a1.ZoneSpec) []cv1a1.ZoneOfferings {
	zoneNames := lo.Map(zones, func(z cv1a1.ZoneSpec, _ int) string { return z.Name })
	zoneOfferings := make([]cv1a1.ZoneOfferings, 0, len(zoneNames))
	
	switch platform {
	case "aws":
		for _, z := range zoneNames {
			zo := buildZoneOfferings(z, []string{"t3", "t4g"})
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
			zo := buildZoneOfferings(z, []string{"e2", "n1"})
			zoneOfferings = append(zoneOfferings, zo)
		}
	default:
		// return []string{}
		return nil
	}

	return zoneOfferings
}


// func availableSvcImage(imgs []map[string]any) int {
// 	if len(imgs) == 0 {
// 		return 0 // Return 0 if no images found
// 	}
// 	imgNames := lo.Map(imgs, func(img map[string]any, _ int) string {
// 		z, ok := img["zone"].(string)
// 		n, ok2 := img["name"].(string)
// 		if !ok || !ok2 || z == "" || n == "" {
// 			return "" // Skip if zone or name is not a string
// 		}
// 		return n
// 	})

// 	// Count the number of services in the images.yaml
// 	return len(imgNames)
// }

// func availableSvcInstanceTypes(s []map[string]any) int {
// 	count := 0
// 	// Assuming each service in the slice has a "zone" key to indicate zonal services
// 	if len(s) == 0 {
// 		return count
// 	}
// 	// Iterate through the slice and count services with "zone" key
// 	zonalSvc := lo.Reduce(s,
// 		func(acc map[string][]string, svc map[string]any, _ int) map[string][]string {
// 			zone, _ := svc["zone"].(string)
// 			if zone == "" {
// 				return acc
// 			}
// 			flavors, ok := svc["flavors"].([]any)
// 			if !ok || len(flavors) == 0 {
// 				return acc
// 			}

// 			// extract non-empty flavor names
// 			names := make([]string, 0, len(flavors))
// 			for _, f := range flavors {
// 				if m, ok := f.(map[string]any); ok {
// 					if name, _ := m["name"].(string); name != "" {
// 						names = append(names, name)
// 					}
// 				}
// 			}
// 			if len(names) > 0 {
// 				acc[zone] = names
// 			}
// 			return acc
// 		}, map[string][]string{})

// 	// Count the number of zonal services
// 	for _, flavors := range zonalSvc {
// 		if len(flavors) > 0 {
// 			count += len(flavors)
// 		}
// 	}
// 	return count
// }


