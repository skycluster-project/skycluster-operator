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
	"strings"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SkyXRDReconciler reconciles a SkyXRD object
type SkyXRDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=skyxrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=skyxrds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=skyxrds/finalizers,verbs=update

// // +kubebuilder:rbac:groups=xrds.skycluster.io,resources=skyproviders,verbs=get;list;watch;create;update;patch;delete

func (r *SkyXRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logName := "SkyXRD"
	logger.Info(fmt.Sprintf("[%s]\tReconciler started for %s", logName, req.Name))

	// Fetch the object
	skyxrd := &corev1alpha1.SkyXRD{}
	if err := r.Get(ctx, req.NamespacedName, skyxrd); err != nil {
		logger.Error(err, fmt.Sprintf("[%s]\tunable to fetch object, maybe it is deleted?", logName))
		return ctrl.Result{}, nil
	}

	for _, xrd := range skyxrd.Spec.Manifests {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(xrd.Manifest), &obj); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\tunable to unmarshal object", logName))
			return ctrl.Result{}, err
		}

		unstrObj := &unstructured.Unstructured{Object: obj}
		unstrObj.SetAPIVersion(xrd.ComponentRef.APIVersion)
		unstrObj.SetKind(xrd.ComponentRef.Kind)
		// The name contains "." which is not allowed when working with xrds
		// We keep the orignial name as the Name field for SkyService object
		// and replace "." with "-" for the object name when we need to work with XRDs
		unstrObj.SetName(strings.Replace(xrd.ComponentRef.Name, ".", "-", -1))
		// unstrObj.SetAnnotations(map[string]string{
		// 	"skycluster.io/pause": "true",
		// })
		if err := ctrl.SetControllerReference(skyxrd, unstrObj, r.Scheme); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\tunable to set controller reference", logName))
			return ctrl.Result{}, err
		}

		// if err := r.Create(ctx, unstrObj); err != nil {
		// 	logger.Error(err, fmt.Sprintf("[%s]\tunable to create object", logName))
		// 	return ctrl.Result{}, err
		// }
		logger.Info(fmt.Sprintf("[%s]\tcreated object [%s]", logName, xrd.ComponentRef.Name))
		break
	}

	// // unstrObj, err := r.GetUnstructuredResource(ctx, "SkyProvider", "xrds.skycluster.io", "v1alpha1", req.Name, req.Namespace)
	// unstrObj := &unstructured.Unstructured{}
	// unstrObj.SetNamespace(req.Namespace)
	// unstrObj.SetName(req.Name)
	// err := r.Get(ctx, req.NamespacedName, unstrObj)
	// gvk := unstrObj.GroupVersionKind()
	// logger.Info(fmt.Sprintf("[SkyXRD]\t%s %v", req.NamespacedName, gvk))
	// if err != nil {
	// 	logger.Info("[SkyXRD]\tunable to fetch object, maybe it is deleted?")
	// 	return ctrl.Result{}, nil
	// }
	// // if SkyProviderObj != nil {
	// // 	logger.Info(fmt.Sprintf("[SkyXRD]\t%s %s", SkyProviderObj.GetName(), SkyProviderObj.GetNamespace()))
	// // }

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyXRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// gvk := schema.GroupVersionKind{
	// 	Group:   "xrds.skycluster.io",
	// 	Version: "v1alpha1",
	// 	Kind:    "SkyProvider",
	// }
	// unstructuredSkyProviderObj := &unstructured.Unstructured{}
	// unstructuredSkyProviderObj.SetGroupVersionKind(gvk)

	// return ctrl.NewControllerManagedBy(mgr).
	// 	// For(&corev1alpha1.SkyXRD{}).
	// 	Watches(
	// 		unstructuredSkyProviderObj,
	// 		&handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	// 	).
	// 	Named("core-skyxrd").
	// 	Complete(r)
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyXRD{}).
		Named("core-skyxrd").
		Complete(r)
}

// func (r *SkyXRDReconciler) GetUnstructuredResource(ctx context.Context, kind, group, version, reqName, reqNamescpae string) (*unstructured.Unstructured, error) {
// 	gvk := schema.GroupVersionKind{
// 		Group:   group,
// 		Version: version,
// 		Kind:    kind,
// 	}

// 	// Create an unstructured object
// 	unstructuredObj := &unstructured.Unstructured{}
// 	unstructuredObj.SetGroupVersionKind(gvk)
// 	newClientKey := client.ObjectKey{
// 		Name:      reqName,
// 		Namespace: reqNamescpae,
// 	}

// 	// Fetch the object using the client
// 	if err := r.Get(ctx, newClientKey, unstructuredObj); err != nil {
// 		// if !errors.IsNotFound(err) {
// 		// 	// Handle the error if it's not a NotFound error
// 		// 	return false, err
// 		// }
// 		return nil, err
// 	}
// 	return unstructuredObj, nil
// }
