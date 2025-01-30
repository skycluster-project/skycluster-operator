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

	// "strconv"

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
	logName := "SkyProvider"
	logger.Info(fmt.Sprintf("[%s]\tReconciler started for %s", logName, req.Name))
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

	type DependencyMap struct {
		Updated       bool
		Created       bool
		Deleted       bool
		DependencyObj *unstructured.Unstructured
	}
	dependenciesMap := []*DependencyMap{}
	depSpecs := SkyDependencies["SkyProvider"]

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
		logger.Info(fmt.Sprintf("[%s]\tUnable to fetch object %s, ns: %s, maybe it is deleted?", logName, req.Name, req.Namespace))
		// Need to delete if the object is within the dependents list of the dependency
		for i := range depSpecs {
			depSpec := &depSpecs[i]
			selector := map[string]string{
				"kind":      depSpec.Kind,
				"group":     depSpec.Group,
				"version":   depSpec.Version,
				"namespace": depSpec.Namespace,
			}
			desc, err := ConvertToMapString(skyProviderDesc)
			if err != nil {
				return ctrl.Result{}, err
			}
			depList, err := ListUnstructuredObjectsByFieldList(r.Client, desc, selector, "spec", "dependedBy")
			if err != nil {
				logger.Error(err, fmt.Sprintf("Unable to retrieve the dependency object for %s", depSpec.Kind))
				return ctrl.Result{}, err
			}
			logger.Info(fmt.Sprintf("[SkyProvider]\t >>> Dependency objects [%d] item founds for %s", len(depList.Items), depSpec.Kind))

			// remove from the dependedBy list, and if the list is empty, remove the dependency object
			skyProviderDescMap, err := ConvertToMapString(skyProviderDesc)
			if err != nil {
				logger.Error(err, fmt.Sprintf("failed to convert %v to map when removing from dependedBy list", skyProviderDesc))
			}
			for i := range depList.Items {
				depObjUnstructured := &depList.Items[i]
				found, idx, err := ContainsNestedMap(depObjUnstructured.Object, skyProviderDescMap, "spec", "dependedBy")
				if err != nil {
					logger.Error(err, fmt.Sprintf("failed to check if depndedBy field has %v in its dependedBy list", skyProviderDescMap))
				}
				if found {
					logger.Info(fmt.Sprintf("[SkyProvider]\t >>> Found idx %d from dependency list with this object in its dependedBy field", idx))
					if err := RemoveFromNestedField(depObjUnstructured.Object, idx, "spec", "dependedBy"); err != nil {
						logger.Error(err, fmt.Sprintf("failed to remove object with idx %d from dependedBy list", idx))
					}
					// if the dependedBy list is empty, flag it to be removed
					m, _ := GetNestedField(depObjUnstructured.Object, "spec")
					if len(m["dependedBy"].([]interface{})) == 0 {
						logger.Info(fmt.Sprintf("[SkyProvider]\t >>> Dep object %s is to be removed.", depObjUnstructured.GetName()))
						if err := r.Delete(ctx, depObjUnstructured); err != nil {
							logger.Error(err, fmt.Sprintf("failed to delete the dependency object %s", depObjUnstructured.GetName()))
						}
					} else { // if it is not empty just update the dependency object
						if err := r.Update(ctx, depObjUnstructured); err != nil {
							logger.Error(err, fmt.Sprintf("failed to update the dependency object %s", depObjUnstructured.GetName()))
						}
					}
				}
			}
		}
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
		logger.Info(fmt.Sprintf("[%s]\tDefault labels do not exist, adding...", logName))
		// Add labels based on the fields
		UpdateLabelsIfDifferent(skyProvider.Labels, map[string]string{
			corev1alpha1.SkyClusterProviderName:   skyProvider.Spec.ProviderRef.ProviderName,
			corev1alpha1.SkyClusterProviderRegion: skyProvider.Spec.ProviderRef.ProviderRegion,
			corev1alpha1.SkyClusterProviderZone:   skyProvider.Spec.ProviderRef.ProviderZone,
			corev1alpha1.SkyClusterProjectID:      uuid.New().String(),
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
	// SearchLables is used to limit the dependencies search to the same provider as the current object
	// may add more labels for more fine-grained search
	searchLabels := map[string]string{
		corev1alpha1.SkyClusterProjectID: skyProvider.Labels[corev1alpha1.SkyClusterProjectID],
	}
	for k, v := range providerLabels {
		searchLabels[k] = v
	}

	if providerType, err := GetProviderTypeFromConfigMap(r.Client, providerLabels); err != nil {
		logger.Error(err, "failed to get provider type from ConfigMap")
		return ctrl.Result{}, err
	} else {
		logger.Info(fmt.Sprintf("[%s]\tAdding provider type label...", logName))
		skyProvider.Spec.ProviderRef.ProviderType = providerType
		skyProvider.Labels[corev1alpha1.SkyClusterProviderType] = providerType
		modified = true
	}

	// Create a list of dependencies objects
	logger.Info(fmt.Sprintf("[%s]\tChecking dependencies for %s...", logName, skyProvider.GetName()))
	for i := range depSpecs {
		depSpec := &depSpecs[i]
		selector := map[string]string{
			"kind":      depSpec.Kind,
			"group":     depSpec.Group,
			"version":   depSpec.Version,
			"namespace": depSpec.Namespace,
		}
		depList, err := ListUnstructuredObjectsByLabels(r.Client, searchLabels, selector)
		if err != nil {
			return ctrl.Result{}, err
		}

		// We allow having multiple replicas of the same type for each dependency
		// i.e. SkyK8S may require multiple SkyVM objects
		logger.Info(fmt.Sprintf("[%s]\t %s: [%d]/[%d] dependency exists.", logName, depSpec.Kind, len(depList.Items), depSpec.Replicas))
		for i := range depList.Items {
			depObj := &depList.Items[i]
			d := &DependencyMap{
				Updated:       false,
				Created:       false,
				Deleted:       false,
				DependencyObj: depObj,
			}
			dependenciesMap = append(dependenciesMap, d)
		}
		// If the number of dependencies is less than the required replicas, create the remaining
		for i := len(depList.Items); i < depSpec.Replicas; i++ {
			depObj, err := r.NewSkyProviderObject(ctx, *skyProvider)
			if err != nil {
				logger.Error(err, "failed to create SkyProvider")
				return ctrl.Result{}, err
			}
			d := DependencyMap{
				Updated:       false,
				Created:       true,
				Deleted:       false,
				DependencyObj: depObj,
			}
			dependenciesMap = append(dependenciesMap, &d)
			logger.Info(fmt.Sprintf("[%s]\t Create a new object (%s)", logName, depSpec.Kind))
		}
	}

	// Dependencies are all retrieved, now we check the depndedBy and dependsOn fields
	logger.Info(fmt.Sprintf("[%s]\tChecking depndedBy/dependsOn fields...", logName))
	skyProviderDescMap, err := ConvertToMapString(skyProviderDesc)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range dependenciesMap {
		t := dependenciesMap[i]
		logger.Info(fmt.Sprintf("[%s]\t - Dependency (%s)", logName, t.DependencyObj.GetName()))
		depObj := t.DependencyObj
		// [DependedBy] field.
		// TODO, ensure an index is returned.
		exists, _, err := ContainsNestedMap(depObj.Object, skyProviderDescMap, "spec", "dependedBy")
		if err != nil {
			logger.Error(err, "")
		}
		if !exists {
			if err := AppendToNestedField(depObj.Object, skyProviderDesc, "spec", "dependedBy"); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to insert into dependedBy list")
			}
			t.Updated = true
		}

		// [DependsOn] field. set the current object as a dependent of the dependency object (core)
		depObjDesc := corev1alpha1.ObjectDescriptor{
			Name:      depObj.GetName(),
			Namespace: depObj.GetNamespace(),
			Kind:      depObj.GetKind(),
			Group:     depObj.GroupVersionKind().Group,
			Version:   depObj.GroupVersionKind().Version,
		}
		if exists, _ := ContainsObjectDescriptor(skyProvider.Spec.DependsOn, depObjDesc); !exists {
			AppendObjectDescriptor(&skyProvider.Spec.DependsOn, depObjDesc)
			modified = true
		}
	}

	// Creation/update of Dependencies objects
	for i := range dependenciesMap {
		d := dependenciesMap[i]
		if d.Deleted {
			logger.Info(fmt.Sprintf("[SkyProvider]\t >>>> Flag deleted is True for dep obj %s", d.DependencyObj.GetName()))
		}
		if d.Created {
			if err := r.Create(ctx, d.DependencyObj); err != nil {
				logger.Error(err, "failed to create dependency object")
				return ctrl.Result{}, err
			}
		} else if d.Updated {
			if err := r.Update(ctx, d.DependencyObj); err != nil {
				logger.Error(err, "failed to update dependency object")
				return ctrl.Result{}, err
			}
		}
	}

	// // if the object is created, we need to update the ConfigMap with current IP CIDR range
	// if _, currentIpSubnet, providerCM, err := getIpCidrPartsFromSkyProvider(r.Client, *skyProvider); err != nil {
	// 	return ctrl.Result{}, errors.Wrap(err, "failed to get IP CIDR parts when updating ConfigMap")
	// } else {
	// 	i, _ := strconv.Atoi(currentIpSubnet)
	// 	if err := updateIPCidrConfigMap(r.Client, providerCM, i+1); err != nil {
	// 		return ctrl.Result{}, errors.Wrap(err, "failed to update ConfigMap")
	// 	}
	// }

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
	// We move the KeyPairRef and SecGroup fields to other objects (such as SkyVM)
	// Because setting up an provider does not necessarily require a keypair or security group
	// if skyProvider.Spec.KeypairRef == nil {
	// 	return nil, errors.New("keypairRef is required")
	// }
	// keypairName := skyProvider.Spec.KeypairRef.Name
	// var secretNamespace string
	// if skyProvider.Spec.KeypairRef.Namespace != "" {
	// 	secretNamespace = skyProvider.Spec.KeypairRef.Namespace
	// } else {
	// 	secretNamespace = skyProvider.Namespace
	// }
	// secret := &corev1.Secret{}
	// if err := r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: keypairName}, secret); err != nil {
	// 	return nil, errors.Wrap(err, "failed to get secret for keypair, does it exist? Same namespace?")
	// }
	// secretData := secret.Data
	// var config map[string]string
	// err := yaml.Unmarshal([]byte(secretData["config"]), &config)
	// if err != nil {
	// 	return nil, errors.Wrap(err, "failed to unmarshal secret data for publicKey")
	// }
	// scpub := strings.TrimSpace(config["publicKey"])
	// if err := SetNestedField(unstructuredObj.Object, scpub, "spec", "forProvider", "publicKey"); err != nil {
	// 	return nil, errors.Wrap(err, "failed to set publicKey")
	// }

	// secGroup := skyProvider.Spec.SecGroup
	// secGroupMap, err := DeepCopyField(secGroup)
	// if err != nil {
	// 	return nil, errors.Wrap(err, "failed to marshal/unmarshal secGroup")
	// }
	// if err := unstructured.SetNestedMap(unstructuredObj.Object, secGroupMap, "spec", "forProvider", "secGroup"); err != nil {
	// 	return nil, errors.Wrap(err, "failed to set secGroup")
	// }

	subnetCidr, _, err := getSubnetCidr(r.Client, skyProvider)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get IP CIDR parts")
	}
	// ipCidrRange := fmt.Sprintf("10.%s.%s.0/24", ipGroup, currentIpSubnet)
	ipCidrRange := subnetCidr
	if err := SetNestedField(unstructuredObj.Object, ipCidrRange, "spec", "forProvider", "ipCidrRange"); err != nil {
		return nil, errors.Wrap(err, "failed to set ipCidrRange")
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

	if skyProvider.Annotations != nil {
		if err := unstructured.SetNestedStringMap(unstructuredObj.Object, skyProvider.Annotations, "metadata", "annotations"); err != nil {
			return nil, errors.Wrap(err, "failed to set annotations")
		}
	}

	if skyProvider.Labels != nil {
		if err := unstructured.SetNestedStringMap(unstructuredObj.Object, skyProvider.Labels, "metadata", "labels"); err != nil {
			return nil, errors.Wrap(err, "failed to set labels")
		}
	}
	// ensure we have provider reference labels setup
	// This block is not needed if we ensure the object always has the required labels
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   skyProvider.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: skyProvider.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   skyProvider.Spec.ProviderRef.ProviderZone,
		corev1alpha1.SkyClusterProviderType:   skyProvider.Spec.ProviderRef.ProviderType,
	}
	UpdateLabelsIfDifferent(unstructuredObj.GetLabels(), providerLabels)

	return unstructuredObj, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyProvider{}).
		Named("core-skyprovider").
		Complete(r)
}
