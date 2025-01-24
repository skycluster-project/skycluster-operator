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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// SkyProviderReconciler reconciles a SkyProvider object
type SkyProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyproviders/finalizers,verbs=update

func (r *SkyProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	modified := false

	// Fetch the object
	var obj corev1alpha1.SkyProvider
	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		logger.Info("[SkyProvider]\tUnable to fetch object, maybe it is deleted?")
		return ctrl.Result{}, nil
	}

	labelKeys := []string{
		corev1alpha1.SkyClusterProviderName,
		corev1alpha1.SkyClusterProviderRegion,
		corev1alpha1.SkyClusterProviderZone,
		corev1alpha1.SkyClusterProviderType,
		corev1alpha1.SkyClusterProjectID,
	}
	if labelExists := LabelsExist(obj.GetLabels(), labelKeys); !labelExists {
		logger.Info("[SkyProvider]\tdefault labels do not exist, adding...")
		// Add labels based on the fields
		modified = CompareAndUpdateLabels(obj.GetLabels(), map[string]string{
			"skycluster.io/provider-name":   obj.Spec.ProviderRef.ProviderName,
			"skycluster.io/provider-region": obj.Spec.ProviderRef.ProviderRegion,
			"skycluster.io/provider-zone":   obj.Spec.ProviderRef.ProviderZone,
			"skycluster.io/project-id":      uuid.New().String(),
		})
	}

	// We will use provider related labels to get the provider type from the ConfigMap
	// and likely use these labels for other dependency objects that may be created
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   obj.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: obj.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   obj.Spec.ProviderRef.ProviderZone,
	}
	if providerType, err := GetProviderTypeFromConfigMap(r.Client, providerLabels); err != nil {
		logger.Error(err, "failed to get provider type from ConfigMap")
		return ctrl.Result{}, err
	} else {
		logger.Info("[SkyProvider]\tAdding provider type label...")
		obj.Spec.ProviderRef.ProviderType = providerType
		obj.Labels[corev1alpha1.SkyClusterProviderType] = providerType
		modified = true
	}

	// Namespaced dependencies
	// Dependencies (claims): [skyproviders.xrds.skycluster.io]
	// List of all the dependencies of type "skyproviders.xrds.skycluster.io"
	// Normally there should be only one SkyProvider object
	searchLabels := providerLabels
	dependenciesList, err := FilterObjectByLabels(r.Client, searchLabels, map[string]string{
		"namespace": obj.Namespace,
		"kind":      "SkyProvider",
		"group":     "xrds.skycluster.io",
		"version":   "v1alpha1",
	})
	if err != nil || len(dependenciesList.Items) == 0 {
		logger.Info("[SkyProvider]\tNo SkyProvider Claims as dependencies")
		if err := r.createSkyProvider(ctx, &obj); err != nil {
			logger.Error(err, "failed to create SkyProvider")
			return ctrl.Result{}, err
		}
		logger.Info("[SkyProvider]\t SkyProvider created")
	} else {
		if len(dependenciesList.Items) > 1 {
			return ctrl.Result{}, errors.New("WARNING: More than one SkyProvider object found. Check this out.")
		}
		logger.Info("[SkyProvider]\tSkyProvider exists, skipping creating, adding a dependent...")
		// Need to retrieve the dependency object, and add the current object into dependents field
		// TODO: I assume there is one SkyProvider object return, if there is more than one,
		// furture checking is needed to underestand why multiple objects exist
		dependencyObj := dependenciesList.Items[0]
		thisObj := corev1alpha1.ObjectSpec{
			Name:      obj.Name,
			Namespace: obj.Namespace,
			Group:     obj.GroupVersionKind().Group,
			Kind:      "SkyProvider",
			Version:   obj.GroupVersionKind().Version,
		}

		if err := InsertNestedField(dependencyObj.Object, thisObj, "spec", "dependents"); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to insert nested field")
		} else {
			if err := r.Update(ctx, &dependencyObj); err != nil {
				logger.Error(err, "failed to update object with project-id")
				return ctrl.Result{}, err
			}

		}

		// switch dependencyObj.Object["spec"].(type) {
		// case map[string]interface{}:
		// 	logger.Info("(1) map[string]interface{}")
		// default:
		// 	logger.Info("type undefined for spec")
		// }
		// if m, ok := dependencyObj.Object["spec"].(map[string]interface{}); ok {
		// 	switch m["dependents"].(type) {
		// 	case map[string]interface{}:
		// 		logger.Info("(2) map[string]interface{}")
		// 	case []interface{}:
		// 		logger.Info("(2) []interface{}")
		// 	case interface{}:
		// 		logger.Info("(2) interface{}")
		// 	default:
		// 		logger.Info("type undefined for dependent")
		// 	}
		// }

		// Add the current object to the dependents field of the dependency object
		// m := dependencyObj.Object
		// AppendToSliceField(m, thisObj, "spec", "dependents")
		// switch dependencyObj.Object["spec"].(type) {
		// case map[string]interface{}:
		// 	logger.Info("map[string]interface{}")
		// case map[string][]interface{}:
		// 	logger.Info("map[string][]interface{}")
		// default:
		// 	logger.Info("default something else")
		// }
		// if valMap, ok := dependencyObj.Object["spec"].(map[string][]interface{}); ok {
		// 	valMap["dependents"] = append(valMap["dependents"], corev1alpha1.ObjectSpec{
		// 		Name:      obj.Name,
		// 		Namespace: obj.Namespace,
		// 		Group:     obj.GroupVersionKind().Group,
		// 		Kind:      reflect.TypeOf(obj).Elem().Name(),
		// 		Version:   obj.GroupVersionKind().Version,
		// 	})
		// 	logger.Info(" -- Dependent added")
		// } else {
		// 	return ctrl.Result{}, errors.New("SkyProvider claim exists, but failed to get dependents list.")
		// }

		// newDepObj := corev1alpha1.ObjectSpec{
		// 	Name:      obj.Name,
		// 	Namespace: obj.Namespace,
		// }
		// We need to check if there are any existing dependents, when we create an object for the first time,
		// this is obviously empty, so we just add a new dependent.
		// depGroupObj := corev1alpha1.ObjectGroupSpec{
		// 	Group: obj.GroupVersionKind().Group,
		// 	Kind:  reflect.TypeOf(obj).Elem().Name(),
		// 	Items: []corev1alpha1.ObjectSpec{newDepObj},
		// }

		// SkyProviderObj := dependenciesObj.Items[0]
		// psObj := SkyProviderObj.Object
		// psSpec := psObj["spec"].(map[string]interface{})
		// dependentsList := psSpec["dependents"].([]interface{})
		// SliceContainObj(dependentsList, )

	}

	// if the SkyProvider obejct is modified, update it
	if modified {
		logger.Info("[SkyProvider]\tSkyProvider updated")
		if err := r.Update(ctx, &obj); err != nil {
			logger.Error(err, "failed to update object with project-id")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *SkyProviderReconciler) createSkyProvider(ctx context.Context, obj *corev1alpha1.SkyProvider) error {
	providerRef := obj.Spec.ProviderRef
	gvk := schema.GroupVersionKind{
		Group:   "xrds.skycluster.io",
		Version: "v1alpha1",
		Kind:    "SkyProvider",
	}
	unstructuredObj := &unstructured.Unstructured{}
	unstructuredObj.SetGroupVersionKind(gvk)
	unstructuredObj.SetNamespace(obj.Namespace)
	unstructuredObj.SetName(obj.Name)

	// Public Key
	// Retrive the secret value and use publicKey field for the xSkyProvider
	secretName := obj.Spec.KeypairRef.Name
	var secretNamespace string
	if obj.Spec.KeypairRef.Namespace != "" {
		secretNamespace = obj.Spec.KeypairRef.Namespace
	} else {
		secretNamespace = obj.Namespace
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret); err != nil {
		return errors.Wrap(err, "failed to get secret")
	}
	secretData := secret.Data
	publicKeyMap := map[string]string{
		"publicKey": string(secretData["publicKey"]),
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, publicKeyMap, "spec", "forProvider"); err != nil {
		return errors.Wrap(err, "failed to set publicKey")
	}

	ipGroup, currentIpSubnet, providerCM, err := getIpCidrPartsFromSkyProvider(r.Client, obj)
	if err != nil {
		return errors.Wrap(err, "failed to get IP CIDR parts")
	}
	ipCidrRangeMap := map[string]string{
		"ipCidrRange": fmt.Sprintf("10.%s.%s.0/24", ipGroup, currentIpSubnet),
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, ipCidrRangeMap, "spec", "forProvider"); err != nil {
		return errors.Wrap(err, "failed to set ipCidrRange")
	}

	secGroup := obj.Spec.SecGroup
	secGroupMap, err := DeepCopyField(secGroup)
	if err != nil {
		return errors.Wrap(err, "failed to marshal/unmarshal secGroup")
	}
	if err := unstructured.SetNestedMap(unstructuredObj.Object, secGroupMap, "spec", "forProvider", "secGroup"); err != nil {
		return errors.Wrap(err, "failed to set secGroup")
	}

	// Set the providerRef field
	providerRefMap, err := DeepCopyField(providerRef)
	if err != nil {
		return errors.Wrap(err, "failed to marshal/unmarshal providerRef")
	}
	if err := unstructured.SetNestedMap(unstructuredObj.Object, providerRefMap, "spec", "providerRef"); err != nil {
		return errors.Wrap(err, "failed to set providerRef")
	}

	// This object is namespaced so let's set the namespace
	if err := unstructured.SetNestedField(unstructuredObj.Object, obj.Namespace, "metadata", "namespace"); err != nil {
		return errors.Wrap(err, "failed to set namespace")
	}

	// Instead of setting owner reference, we set fields "dependsOn" and "dependents"
	// Because we may want to keep this object even if the SkyProvider object is deleted.
	// Another controller (SkyXRD) will be responsible for deleting this object,
	// when it is no longer needed (i.e. there is no dependents).
	// We need to set dependent field in SkyProvider and dependsOn field in the SkyProvider object
	// SkyProvider -- depends on --> SkyProvider
	dependentObj := corev1alpha1.ObjectSpec{
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Group:     obj.GroupVersionKind().Group,
		Kind:      reflect.TypeOf(obj).Elem().Name(),
		Version:   obj.GroupVersionKind().Version,
	}
	// We need to check if there are any existing dependents, when we create an object for the first time,
	// this is obviously empty, so we just add a new dependent.
	if err := InsertNestedField(unstructuredObj.Object, dependentObj, "spec", "dependents"); err != nil {
		return errors.Wrap(err, "failed to insert dependent object into the list")
	}

	annot := map[string]string{
		"crossplane.io/paused": "true",
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, annot, "metadata", "annotations"); err != nil {
		return errors.Wrap(err, "failed to set annotations")
	}
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   obj.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: obj.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   obj.Spec.ProviderRef.ProviderZone,
		corev1alpha1.SkyClusterProviderType:   obj.Spec.ProviderRef.ProviderType,
		corev1alpha1.SkyClusterProjectID:      obj.Labels[corev1alpha1.SkyClusterProjectID],
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, providerLabels, "metadata", "labels"); err != nil {
		return errors.Wrap(err, "failed to set labels")
	}

	// Create the XSkyProvider
	if err := r.Create(ctx, unstructuredObj); err != nil {
		return errors.Wrap(err, "failed to create XSkyProvider")
	}
	// if the object is created, we need to update the ConfigMap with current IP CIDR range
	if err = updateIPCidrConfigMap(r.Client, providerCM, currentIpSubnet); err != nil {
		return errors.Wrap(err, "failed to update ConfigMap")
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyProvider{}).
		Named("core-skyprovider").
		Complete(r)
}
