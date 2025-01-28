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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/google/uuid"
)

// SkyVMReconciler reconciles a SkyVM object
type SkyVMReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyvms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyvms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyvms/finalizers,verbs=update

func (r *SkyVMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logName := "SkyVM"
	logger.Info(fmt.Sprintf("[%s]\tReconciler started for %s", logName, req.Name))
	modified := false

	dependenciesMap := []*SkyVMDependencyMap{}
	depSpecs := SkyDependencies["SkyVM"]

	// Check dependencies for the current object
	skyVMDesc := corev1alpha1.ObjectDescriptor{
		Name:      req.Name,
		Namespace: req.Namespace,
		Kind:      "SkyVM",
		Group:     corev1alpha1.SkyClusterCoreGroup,
		Version:   corev1alpha1.SkyClusterVersion,
	}

	// Fetch the object
	var skyVM corev1alpha1.SkyVM
	if err := r.Get(ctx, req.NamespacedName, &skyVM); err != nil {
		logger.Info(fmt.Sprintf("[%s]\tUnable to fetch object %s, ns: %s, maybe it is deleted?", logName, req.Name, req.Namespace))
		// Need to delete if the object is within the dependents list of the dependency
		// We need to find dependencies, we need to iterate through all objects
		// and check their dependedBy field to see if the current object is in the list
		for i := range depSpecs {
			depSpec := &depSpecs[i]
			selector := map[string]string{
				"kind":      depSpec.Kind,
				"group":     depSpec.Group,
				"version":   depSpec.Version,
				"namespace": depSpec.Namespace,
			}
			var searchLabels = map[string]string{} // empty search labels, because we don't have the object anymore
			depList, err := ListSkyProviderByLabels(r.Client, searchLabels, selector)
			if err != nil {
				logger.Error(err, fmt.Sprintf("Unable to retrieve the dependency object for %s", depSpec.Kind))
				return ctrl.Result{}, err
			}
			logger.Info(fmt.Sprintf("[%s]\t >>> Dependency objects [%d] item founds for %s", logName, len(depList.Items), depSpec.Kind))
			for i := range depList.Items {
				depObj := &depList.Items[i]
				exists, idx := ContainsObjectDescriptor(depObj.Spec.DependedBy, skyVMDesc)
				if exists {
					logger.Info(fmt.Sprintf("[%s]\t >>> Found idx %d from dependency list with this object in its dependedBy field", logName, idx))
					RemoveObjectDescriptor(&depObj.Spec.DependedBy, idx)
					if len(depObj.Spec.DependedBy) == 0 {
						logger.Info(fmt.Sprintf("[%s]\t >>> Dep object %s is to be removed.", logName, depObj.GetName()))
						if err := r.Delete(ctx, depObj); err != nil {
							logger.Error(err, fmt.Sprintf("failed to delete the dependency object %s", depObj.GetName()))
						}
					} else {
						if err := r.Update(ctx, depObj); err != nil {
							logger.Error(err, fmt.Sprintf("failed to update the dependency object %s", depObj.GetName()))
						}
					}
				}
			}
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	labelKeys := []string{
		corev1alpha1.SkyClusterProviderName,
		corev1alpha1.SkyClusterProviderRegion,
		corev1alpha1.SkyClusterProviderZone,
		corev1alpha1.SkyClusterProviderType,
		corev1alpha1.SkyClusterProjectID,
	}
	if labelExists := ContainsLabels(skyVM.GetLabels(), labelKeys); !labelExists {
		logger.Info(fmt.Sprintf("[%s]\tDefault labels do not exist, adding...", logName))
		// Add labels based on the fields
		UpdateLabelsIfDifferent(&skyVM.Labels, map[string]string{
			"skycluster.io/provider-name":   skyVM.Spec.ProviderRef.ProviderName,
			"skycluster.io/provider-region": skyVM.Spec.ProviderRef.ProviderRegion,
			"skycluster.io/provider-zone":   skyVM.Spec.ProviderRef.ProviderZone,
			"skycluster.io/project-id":      uuid.New().String(),
		})
		modified = true
	}

	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   skyVM.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: skyVM.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   skyVM.Spec.ProviderRef.ProviderZone,
	}
	searchLabels := map[string]string{
		corev1alpha1.SkyClusterProjectID: skyVM.Labels[corev1alpha1.SkyClusterProjectID],
	}
	for k, v := range providerLabels {
		searchLabels[k] = v
	}

	if providerType, err := GetProviderTypeFromConfigMap(r.Client, providerLabels); err != nil {
		logger.Error(err, "failed to get provider type from ConfigMap")
		return ctrl.Result{}, err
	} else {
		logger.Info(fmt.Sprintf("[%s]\tAdding provider type label...", logName))
		skyVM.Spec.ProviderRef.ProviderType = providerType
		skyVM.Labels[corev1alpha1.SkyClusterProviderType] = providerType
		modified = true
	}

	// Create a list of dependencies objects
	logger.Info(fmt.Sprintf("[%s]\tChecking dependencies for %s...", logName, skyVM.GetName()))
	for i := range depSpecs {
		depSpec := &depSpecs[i]
		selector := map[string]string{
			"kind":      depSpec.Kind,
			"group":     depSpec.Group,
			"version":   depSpec.Version,
			"namespace": depSpec.Namespace,
		}
		depList, err := ListSkyProviderByLabels(r.Client, searchLabels, selector)
		if err != nil {
			return ctrl.Result{}, err
		}

		logger.Info(fmt.Sprintf("[%s]\t %s: [%d]/[%d] dependency exists.", logName, depSpec.Kind, len(depList.Items), depSpec.Replicas))
		for i := range depList.Items {
			depObj := &depList.Items[i]
			d := &SkyVMDependencyMap{
				Updated:        false,
				Created:        false,
				Deleted:        false,
				SkyProviderObj: depObj,
			}
			dependenciesMap = append(dependenciesMap, d)
		}
		// If the number of dependencies is less than the required replicas, create the remaining
		for i := len(depList.Items); i < depSpec.Replicas; i++ {
			depObj, err := r.NewSkyProviderObject(ctx, &skyVM)
			if err != nil {
				logger.Error(err, "failed to create SkyProvider")
				return ctrl.Result{}, err
			}
			d := SkyVMDependencyMap{
				Updated:        false,
				Created:        true,
				Deleted:        false,
				SkyProviderObj: depObj,
			}
			dependenciesMap = append(dependenciesMap, &d)
			logger.Info(fmt.Sprintf("[%s]\t Create a new object (%s)", logName, depSpec.Kind))
		}
	}

	// Dependencies are all retrieved, now we check the depndedBy and dependsOn fields
	logger.Info(fmt.Sprintf("[%s]\tChecking depndedBy/dependsOn fields...", logName))
	for i := range dependenciesMap {
		t := dependenciesMap[i]
		depObj := t.SkyProviderObj
		// [DependedBy] field.
		if exists, _ := ContainsObjectDescriptor(depObj.Spec.DependedBy, skyVMDesc); !exists {
			AppendObjectDescriptor(&depObj.Spec.DependedBy, skyVMDesc)
			t.Updated = true
		}

		// [DependsOn] field. set the current object as a dependent of the dependency object (core)
		depObjDesc := corev1alpha1.ObjectDescriptor{
			Name:      depObj.Name,
			Namespace: depObj.Namespace,
			Kind:      depObj.Kind,
			Group:     depObj.GroupVersionKind().Group,
			Version:   depObj.GroupVersionKind().Version,
		}
		logger.Info(fmt.Sprintf("[%s]\t - Dependency: %s [%s] [%s] [%s]", logName, depObj.Name, depObj.Namespace, depObj.Kind, depObj.GroupVersionKind().Group))
		if exists, _ := ContainsObjectDescriptor(skyVM.Spec.DependsOn, depObjDesc); !exists {
			AppendObjectDescriptor(&skyVM.Spec.DependsOn, depObjDesc)
			modified = true
		}
	}

	// Creation/update of Dependencies objects
	for i := range dependenciesMap {
		d := dependenciesMap[i]
		if d.Deleted {
			if d.SkyProviderObj != nil {
				logger.Info(fmt.Sprintf("[%s]\t >>>> Flag deleted is True for dep obj %s", logName, d.SkyProviderObj.GetName()))
			}
		}
		if d.Created {
			if d.SkyProviderObj != nil {
				if err := r.Create(ctx, d.SkyProviderObj); err != nil {
					logger.Error(err, "failed to create dependency object")
					return ctrl.Result{}, err
				}
			}
		} else if d.Updated {
			if d.SkyProviderObj != nil {
				if err := r.Update(ctx, d.SkyProviderObj); err != nil {
					logger.Error(err, "failed to update dependency object")
					return ctrl.Result{}, err
				}
			}
		}
	}

	// if the SkyProvider obejct is modified, update it
	if modified {
		logger.Info(fmt.Sprintf("[%s]\tSkyVM updated", logName))
		if err := r.Update(ctx, &skyVM); err != nil {
			logger.Error(err, "failed to update object with project-id")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *SkyVMReconciler) NewSkyProviderObject(ctx context.Context, skyVM *corev1alpha1.SkyVM) (*corev1alpha1.SkyProvider, error) {

	skyProvider := &corev1alpha1.SkyProvider{}
	skyProvider.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   corev1alpha1.SkyClusterCoreGroup,
		Version: corev1alpha1.SkyClusterVersion,
		Kind:    "SkyProvider",
	})
	skyProvider.SetNamespace(skyVM.Namespace)
	skyProvider.SetName(skyVM.Name)

	// Public Key
	// Retrive the secret value and use publicKey field for the xSkyProvider
	skyProvider.Spec.KeypairRef = skyVM.Spec.KeypairRef

	// If we are creating a new SkyProvider object, we adopt the current security group
	// TODO: If the SkyProvider object already exists, we should
	// add an object corresponding to the security group
	if skyVM.Spec.SecGroup != nil {
		skyProvider.Spec.SecGroup = *skyVM.Spec.SecGroup
	} else {
		// some default security group
		secgroup := corev1alpha1.SecGroup{
			TCPPorts: []corev1alpha1.PortSpec{
				{
					FromPort: 22,
					ToPort:   22,
				},
			},
		}
		skyProvider.Spec.SecGroup = secgroup
	}

	// Set the providerRef field
	providerRef := skyVM.Spec.ProviderRef
	skyProvider.Spec.ProviderRef = providerRef

	annot := map[string]string{
		"crossplane.io/paused": "true",
	}
	skyProvider.SetAnnotations(annot)
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   skyVM.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: skyVM.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   skyVM.Spec.ProviderRef.ProviderZone,
		corev1alpha1.SkyClusterProviderType:   skyVM.Spec.ProviderRef.ProviderType,
		corev1alpha1.SkyClusterProjectID:      skyVM.Labels[corev1alpha1.SkyClusterProjectID],
	}
	skyProvider.SetLabels(providerLabels)

	return skyProvider, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyVMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyVM{}).
		Named("core-skyvm").
		Complete(r)
}
