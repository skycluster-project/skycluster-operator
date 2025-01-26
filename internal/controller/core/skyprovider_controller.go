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

	// Dependencies: Objects can have dependencies on other objects
	// Dependencies are defined in the SkyDependencies map
	// Where for each type (i.e. SkyProvider), a list of dependencies is defined
	// Each dependency is defined by its Kind, Group, Version, and Replicas
	// Where Replicas is the number of instances of the dependency object that should exist
	// When the object is created, the dependency objects are created as well
	// The object details are added to the depndedBy field of the dependency object
	// Similarly, the dependency object details are added to the dependsOn field of the object
	// When the object is deleted, the object details are removed from the
	// depndedBy field of the dependency object,
	// and if the depndedBy field is empty, the dependency object is deleted

	depSpecs := SkyDependencies["SkyProvider"]
	depSpecsCopy := make([]SkyDependency, len(depSpecs))
	copy(depSpecsCopy, depSpecs)
	for i := range depSpecsCopy {
		depSpec := &depSpecsCopy[i]
		depSpec.Namespace, depSpec.Created, depSpec.Updated, depSpec.Deleted = req.Namespace, false, false, false
		// // intentionally setting the name of each dependency as the name of this object.
		//// depSpec.Name = req.Name
	}

	// Check dependencies for the current object
	skyProviderDesc := corev1alpha1.ObjectDescriptor{
		Name:      req.Name,
		Namespace: req.Namespace,
		Kind:      "SkyProvider",
		Group:     corev1alpha1.SkyClusterCoreGroup,
		Version:   corev1alpha1.SkyClusterVersion,
	}

	// Fetch the object
	skyProvider := &corev1alpha1.SkyProvider{}

	if err := r.Get(ctx, req.NamespacedName, skyProvider); err != nil {
		logger.Info(fmt.Sprintf("[SkyProvider]\tUnable to fetch object %s, ns: %s, maybe it is deleted?", req.Name, req.Namespace))
		// Need to delete if the object is within the dependents list of the dependency
		// for i := range depSpecsCopy {
		// 	// depSpec := &depSpecsCopy[i]
		// 	// Get the dependency object,
		// 	// TODO: we only support unstructured objects for now.
		// 	// // What is the depedency if an object of a CRD?
		// 	// if depObjUnstructured, err := GetUnstructuredObject(r.Client, depSpec.Name, depSpec.Namespace); err != nil {
		// 	// 	logger.Error(err, fmt.Sprintf("Unable to retrieve the dependency object %s in ns: %s", depSpec.Name, depSpec.Namespace))
		// 	// } else {
		// 	// 	logger.Info(fmt.Sprintf("[SkyProvider]\t >>> Dependency object %s in ns: %s exists, checking dependedBy fields", depSpec.Name, depSpec.Namespace))
		// 	// 	// remove from the dependedBy list, and if the list is empty, remove the dependency object
		// 	// 	// Check dependencies for the current object
		// 	// 	skyProviderDesc := corev1alpha1.ObjectDescriptor{
		// 	// 		Name:      req.Name,
		// 	// 		Namespace: req.Namespace,
		// 	// 		Kind:      "SkyProvider",
		// 	// 		Group:     corev1alpha1.SkyClusterCoreGroup,
		// 	// 		Version:   corev1alpha1.SkyClusterCoreGroup,
		// 	// 	}
		// 	// 	skyProviderDescMap, err := DeepCopyToMapString(skyProviderDesc)
		// 	// 	if err != nil {
		// 	// 		logger.Error(err, fmt.Sprintf("failed to convert %v to map when removing from dependedBy list", skyProviderDesc))
		// 	// 	}
		// 	// 	found, idx, err := ContainsNestedMap(depObjUnstructured.Object, skyProviderDescMap, "spec", "dependedBy")
		// 	// 	if err != nil {
		// 	// 		logger.Error(err, fmt.Sprintf("failed to check if the object with skyProviderDescMap as %v exists in the dependedBy list", skyProviderDescMap))
		// 	// 	}
		// 	// 	logger.Info(fmt.Sprintf("[SkyProvider]\t >>> Found idx %d from dependency list with this object in its dependedBy field", idx))
		// 	// 	if found {
		// 	// 		if err := RemoveFromNestedField(depObjUnstructured.Object, idx, "spec", "dependedBy"); err != nil {
		// 	// 			logger.Error(err, fmt.Sprintf("failed to remove object with idx %d from dependedBy list", idx))
		// 	// 		}
		// 	// 		logger.Info(fmt.Sprintf("[SkyProvider]\t >>> Removed object from dependedBy list"))
		// 	// 		depSpec.Updated = true
		// 	// 		// if the dependedBy list is empty, remove the dependency object
		// 	// 		m, _ := GetNestedField(depObjUnstructured.Object, "spec")
		// 	// 		if len(m["dependedBy"].([]interface{})) == 0 {
		// 	// 			if err := r.Delete(ctx, depObjUnstructured); err != nil {
		// 	// 				logger.Error(err, fmt.Sprintf("failed to delete the dependency object %s in ns: %s", depSpec.Name, depSpec.Namespace))
		// 	// 			}
		// 	// 			logger.Info(fmt.Sprintf("[SkyProvider]\t >>> No other objects in this depSpec. Deleted"))
		// 	// 			depSpec.Deleted = true
		// 	// 		}
		// 	// 		logger.Info(fmt.Sprintf("[SkyProvider]\t >>> More than one dependencies exist. Skip deleting"))
		// 	// 	}
		// 	// }
		// }
		return ctrl.Result{}, nil
	}

	labelKeys := []string{
		corev1alpha1.SkyClusterProviderName,
		corev1alpha1.SkyClusterProviderRegion,
		corev1alpha1.SkyClusterProviderZone,
		corev1alpha1.SkyClusterProviderType,
		corev1alpha1.SkyClusterProjectID,
	}
	if labelExists := ContainsLabels(skyProvider.GetLabels(), labelKeys); !labelExists {
		logger.Info("[SkyProvider]\tdefault labels do not exist, adding...")
		// Add labels based on the fields
		UpdateLabelsIfDifferent(&skyProvider.Labels, map[string]string{
			"skycluster.io/provider-name":   skyProvider.Spec.ProviderRef.ProviderName,
			"skycluster.io/provider-region": skyProvider.Spec.ProviderRef.ProviderRegion,
			"skycluster.io/provider-zone":   skyProvider.Spec.ProviderRef.ProviderZone,
			"skycluster.io/project-id":      uuid.New().String(),
		})
		modified = true
	}

	// We will use provider related labels to get the provider type from the ConfigMap
	// and likely use these labels for other dependency objects that may be created
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   skyProvider.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: skyProvider.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   skyProvider.Spec.ProviderRef.ProviderZone,
	}
	if providerType, err := GetProviderTypeFromConfigMap(r.Client, providerLabels); err != nil {
		logger.Error(err, "failed to get provider type from ConfigMap")
		return ctrl.Result{}, err
	} else {
		logger.Info("[SkyProvider]\tAdding provider type label...")
		skyProvider.Spec.ProviderRef.ProviderType = providerType
		skyProvider.Labels[corev1alpha1.SkyClusterProviderType] = providerType
		modified = true
	}

	// Dependencies should have the same provider labels as the current object
	searchLabels := providerLabels
	dependenciesMap := map[string]*unstructured.Unstructured{}
	// Create a list of dependencies objects
	for i := range depSpecsCopy {
		var depObj *unstructured.Unstructured
		depSpec := &depSpecsCopy[i]
		s := map[string]string{
			"kind":      depSpec.Kind,
			"group":     depSpec.Group,
			"version":   depSpec.Version,
			"namespace": depSpec.Namespace,
		}
		if depList, err := ListUnstructuredObjectsByLabels(r.Client, searchLabels, s); err != nil {
			return ctrl.Result{}, err
		} else if len(depList.Items) == 1 { // THE depenedency exists
			logger.Info(fmt.Sprintf("[SkyProvider]\t%s dependency exists", depSpec.Kind))
			depObj = &depList.Items[0] // Need to inset the current object into the dependents list of this dependency
			// logger.Info(fmt.Sprintf("[SkyProvider]\tFound: %s %s", depObj.GetName(), depObj.Object["spec"]))
		} else if len(depList.Items) == 0 { // No dependency exists, create one
			logger.Info("[SkyProvider]\tNo SkyProvider Claims as dependencies, creating one...")
			depObj, err = r.NewSkyProviderObject(ctx, *skyProvider)
			if err != nil {
				logger.Error(err, "failed to create SkyProvider")
				return ctrl.Result{}, err
			}
			depSpec.Created = true
		} else {
			// We only support one dependency of a single type for now
			return ctrl.Result{}, errors.New("SkyProvider claim exists, the dependency list constains more than one or zero elements.")
		}

		// Inserting into dependedBy list (xrds)
		logger.Info("[SkyProvider]\tAppending into dependedBy list...")
		if skyProviderDescMap, err := DeepCopyToMapString(skyProviderDesc); err != nil {
			return ctrl.Result{}, err
		} else {
			exists, _, err := ContainsNestedMap(depObj.Object, skyProviderDescMap, "spec", "dependedBy")
			if err != nil {
				logger.Error(err, "")
			}
			if !exists {
				if err := AppendToNestedField(depObj.Object, skyProviderDesc, "spec", "dependedBy"); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "failed to insert into dependedBy list")
				}
				depSpec.Updated = true
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
		if exists := ObjectDescriptorExists(skyProvider.Spec.DependsOn, depObjDesc); !exists {
			logger.Info("[SkyProvider]\t  Does not exist, Appending into list...")
			AppendObjectDescriptor(&skyProvider.Spec.DependsOn, depObjDesc)
			modified = true
		}
		depSpec.Name = depSpec.Kind + "-" + depSpec.Group + "-" + depSpec.Namespace
		dependenciesMap[depSpec.Name] = depObj
	}

	// Creation/update of Dependencies objects
	for _, depSpec := range depSpecsCopy {
		if depSpec.Deleted {
			continue
		}
		if depSpec.Created {
			if err := r.Create(ctx, dependenciesMap[depSpec.Name]); err != nil {
				logger.Error(err, "failed to create dependency object")
				return ctrl.Result{}, err
			}
		} else if depSpec.Updated {
			if err := r.Update(ctx, dependenciesMap[depSpec.Name]); err != nil {
				logger.Error(err, "failed to update dependency object")
				return ctrl.Result{}, err
			}
		}
	}

	// if the object is created, we need to update the ConfigMap with current IP CIDR range
	if _, currentIpSubnet, providerCM, err := getIpCidrPartsFromSkyProvider(r.Client, *skyProvider); err != nil {
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
		if err := r.Update(ctx, skyProvider); err != nil {
			logger.Error(err, "failed to update object with project-id")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *SkyProviderReconciler) NewSkyProviderObject(ctx context.Context, skyProvider corev1alpha1.SkyProvider) (*unstructured.Unstructured, error) {
	providerRef := skyProvider.Spec.ProviderRef
	gvk := schema.GroupVersionKind{
		Group:   "xrds.skycluster.io",
		Version: "v1alpha1",
		Kind:    "SkyProvider",
	}
	unstructuredObj := &unstructured.Unstructured{}
	unstructuredObj.SetGroupVersionKind(gvk)
	unstructuredObj.SetNamespace(skyProvider.Namespace)
	unstructuredObj.SetName(skyProvider.Name)

	// Public Key
	// Retrive the secret value and use publicKey field for the xSkyProvider
	keypairName := skyProvider.Spec.KeypairRef.Name
	var secretNamespace string
	if skyProvider.Spec.KeypairRef.Namespace != "" {
		secretNamespace = skyProvider.Spec.KeypairRef.Namespace
	} else {
		secretNamespace = skyProvider.Namespace
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: keypairName}, secret); err != nil {
		return nil, errors.Wrap(err, "failed to get secret")
	}
	secretData := secret.Data
	publicKeyMap := map[string]string{
		"publicKey": string(secretData["publicKey"]),
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, publicKeyMap, "spec", "forProvider"); err != nil {
		return nil, errors.Wrap(err, "failed to set publicKey")
	}

	ipGroup, currentIpSubnet, _, err := getIpCidrPartsFromSkyProvider(r.Client, skyProvider)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get IP CIDR parts")
	}
	ipCidrRangeMap := map[string]string{
		"ipCidrRange": fmt.Sprintf("10.%s.%s.0/24", ipGroup, currentIpSubnet),
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, ipCidrRangeMap, "spec", "forProvider"); err != nil {
		return nil, errors.Wrap(err, "failed to set ipCidrRange")
	}

	secGroup := skyProvider.Spec.SecGroup
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
	if err := unstructured.SetNestedField(unstructuredObj.Object, skyProvider.Namespace, "metadata", "namespace"); err != nil {
		return nil, errors.Wrap(err, "failed to set namespace")
	}

	annot := map[string]string{
		"crossplane.io/paused": "true",
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, annot, "metadata", "annotations"); err != nil {
		return nil, errors.Wrap(err, "failed to set annotations")
	}
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   skyProvider.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: skyProvider.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   skyProvider.Spec.ProviderRef.ProviderZone,
		corev1alpha1.SkyClusterProviderType:   skyProvider.Spec.ProviderRef.ProviderType,
		corev1alpha1.SkyClusterProjectID:      skyProvider.Labels[corev1alpha1.SkyClusterProjectID],
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
