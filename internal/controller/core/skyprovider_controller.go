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
	"strconv"

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
	logger.Info(fmt.Sprintf("[SkyProvider] Reconciler started for %s", req.Name))
	modified := false

	// Dependencies: Normally there should be only one Kind per group
	// And I assume only one dependency of a single type for now
	depSpecList := SkyDependencies["SkyProvider"]
	depSpecDetailedList := []SkyDependency{}
	copy(depSpecDetailedList, depSpecList)
	for _, dep := range depSpecDetailedList {
		dep.Namespace, dep.Created, dep.Updated = req.Namespace, false, false
	}

	// Fetch the object
	obj := &corev1alpha1.SkyProvider{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		logger.Info("[SkyProvider]\tUnable to fetch object, maybe it is deleted?")
		// Need to delete if the object is within the dependents list of the dependency
		return ctrl.Result{}, nil
	}

	labelKeys := []string{
		corev1alpha1.SkyClusterProviderName,
		corev1alpha1.SkyClusterProviderRegion,
		corev1alpha1.SkyClusterProviderZone,
		corev1alpha1.SkyClusterProviderType,
		corev1alpha1.SkyClusterProjectID,
	}
	if labelExists := ContainsLabels(obj.GetLabels(), labelKeys); !labelExists {
		logger.Info("[SkyProvider]\tdefault labels do not exist, adding...")
		// Add labels based on the fields
		UpdateLabelsIfDifferent(&obj.Labels, map[string]string{
			"skycluster.io/provider-name":   obj.Spec.ProviderRef.ProviderName,
			"skycluster.io/provider-region": obj.Spec.ProviderRef.ProviderRegion,
			"skycluster.io/provider-zone":   obj.Spec.ProviderRef.ProviderZone,
			"skycluster.io/project-id":      uuid.New().String(),
		})
		modified = true
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

	// Check dependencies for the current object
	objDesc := corev1alpha1.ObjectDescriptor{
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Kind:      "SkyProvider",
		Group:     obj.GroupVersionKind().Group,
		Version:   obj.GroupVersionKind().Version,
	}
	// Dependencies should have the same provider labels as the current object
	searchLabels := providerLabels

	dependenciesObjectList := map[string]*unstructured.Unstructured{}

	for _, dep := range depSpecDetailedList {
		var depObj *unstructured.Unstructured

		s := map[string]string{
			"kind":      dep.Kind,
			"group":     dep.Group,
			"namespace": dep.Namespace,
		}
		if depList, err := ListUnstructuredObjectsByLabels(r.Client, searchLabels, s); err != nil {
			return ctrl.Result{}, err
		} else if len(depList.Items) == 1 { // THE depenedency exists
			logger.Info(fmt.Sprintf("[SkyProvider]\t%s dependency exists", dep.Kind))
			depObj = &depList.Items[0] // Need to inset the current object into the dependents list of this dependency
			// logger.Info(fmt.Sprintf("[SkyProvider]\tFound: %s %s", depObj.GetName(), depObj.Object["spec"]))
		} else if len(depList.Items) == 0 { // No dependency exists, create one
			logger.Info("[SkyProvider]\tNo SkyProvider Claims as dependencies, creating one...")
			depObj, err = r.NewSkyProviderObject(ctx, *obj)
			if err != nil {
				logger.Error(err, "failed to create SkyProvider")
				return ctrl.Result{}, err
			}
			dep.Created = true
		} else {
			// We only support one dependency of a single type for now
			return ctrl.Result{}, errors.New("SkyProvider claim exists, the dependency list constains more than one or zero elements.")
		}

		// Inserting into dependedBy list (xrds)
		logger.Info("[SkyProvider]\tAppending into dependedBy list...")
		if objDescMap, err := DeepCopyToMapString(objDesc); err != nil {
			return ctrl.Result{}, err
		} else {
			exists, err := ContainsNestedMap(depObj.Object, objDescMap, "spec", "dependedBy")
			if err != nil {
				logger.Error(err, "")
			}
			if !exists {
				if err := AppendNestedField(depObj.Object, objDesc, "spec", "dependedBy"); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "failed to insert into dependedBy list")
				}
				dep.Updated = true
			}
		}

		// set the current object as a dependent of the dependency object (core)
		logger.Info("[SkyProvider]\tAppending into dependsOn list...")
		depObjDesc := corev1alpha1.ObjectDescriptor{
			Name:      depObj.GetName(),
			Namespace: depObj.GetNamespace(),
			Kind:      depObj.GetKind(),
			Group:     depObj.GroupVersionKind().Group,
			Version:   depObj.GroupVersionKind().Version,
		}
		if exists := ObjectDescriptorExists(obj.Spec.DependsOn, depObjDesc); !exists {
			logger.Info("[SkyProvider]\t  Does not exist, Appending into list...")
			AppendObjectDescriptor(&obj.Spec.DependsOn, depObjDesc)
			modified = true
		}
		dep.Name = dep.Kind + "-" + dep.Group + "-" + dep.Namespace
		dependenciesObjectList[dep.Name] = depObj
	}

	// Creation/update of Dependencies objects
	for _, dep := range depSpecDetailedList {

		if dep.Created {
			if err := r.Create(ctx, dependenciesObjectList[dep.Name]); err != nil {
				logger.Error(err, "failed to create dependency object")
				return ctrl.Result{}, err
			}
		} else if dep.Updated {
			if err := r.Update(ctx, dependenciesObjectList[dep.Name]); err != nil {
				logger.Error(err, "failed to update dependency object")
				return ctrl.Result{}, err
			}
		}
	}

	// if the object is created, we need to update the ConfigMap with current IP CIDR range
	if _, currentIpSubnet, providerCM, err := getIpCidrPartsFromSkyProvider(r.Client, *obj); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to get IP CIDR parts when updating ConfigMap")
	} else {
		i, _ := strconv.Atoi(currentIpSubnet)
		if err := updateIPCidrConfigMap(r.Client, providerCM, i+1); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update ConfigMap")
		}
	}

	// if the SkyProvider obejct is modified, update it
	if modified {
		logger.Info("[SkyProvider]\tSkyProvider updated")
		if err := r.Update(ctx, obj); err != nil {
			logger.Error(err, "failed to update object with project-id")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *SkyProviderReconciler) NewSkyProviderObject(ctx context.Context, obj corev1alpha1.SkyProvider) (*unstructured.Unstructured, error) {
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
		return nil, errors.Wrap(err, "failed to get secret")
	}
	secretData := secret.Data
	publicKeyMap := map[string]string{
		"publicKey": string(secretData["publicKey"]),
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, publicKeyMap, "spec", "forProvider"); err != nil {
		return nil, errors.Wrap(err, "failed to set publicKey")
	}

	ipGroup, currentIpSubnet, _, err := getIpCidrPartsFromSkyProvider(r.Client, obj)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get IP CIDR parts")
	}
	ipCidrRangeMap := map[string]string{
		"ipCidrRange": fmt.Sprintf("10.%s.%s.0/24", ipGroup, currentIpSubnet),
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, ipCidrRangeMap, "spec", "forProvider"); err != nil {
		return nil, errors.Wrap(err, "failed to set ipCidrRange")
	}

	secGroup := obj.Spec.SecGroup
	secGroupMap, err := DeepCopyField(secGroup)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal/unmarshal secGroup")
	}
	if err := unstructured.SetNestedMap(unstructuredObj.Object, secGroupMap, "spec", "forProvider", "secGroup"); err != nil {
		return nil, errors.Wrap(err, "failed to set secGroup")
	}

	// Set the providerRef field
	providerRefMap, err := DeepCopyField(providerRef)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal/unmarshal providerRef")
	}
	if err := unstructured.SetNestedMap(unstructuredObj.Object, providerRefMap, "spec", "providerRef"); err != nil {
		return nil, errors.Wrap(err, "failed to set providerRef")
	}

	// This object is namespaced so let's set the namespace
	if err := unstructured.SetNestedField(unstructuredObj.Object, obj.Namespace, "metadata", "namespace"); err != nil {
		return nil, errors.Wrap(err, "failed to set namespace")
	}

	annot := map[string]string{
		"crossplane.io/paused": "true",
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, annot, "metadata", "annotations"); err != nil {
		return nil, errors.Wrap(err, "failed to set annotations")
	}
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   obj.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: obj.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   obj.Spec.ProviderRef.ProviderZone,
		corev1alpha1.SkyClusterProviderType:   obj.Spec.ProviderRef.ProviderType,
		corev1alpha1.SkyClusterProjectID:      obj.Labels[corev1alpha1.SkyClusterProjectID],
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, providerLabels, "metadata", "labels"); err != nil {
		return nil, errors.Wrap(err, "failed to set labels")
	}

	return unstructuredObj, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyProvider{}).
		Named("core-skyprovider").
		Complete(r)
}
