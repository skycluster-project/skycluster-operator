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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SkyXRDReconciler reconciles a SkyXRD object
type SkyXRDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=skyxrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=skyxrds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=skyxrds/finalizers,verbs=update

// +kubebuilder:rbac:groups=xrds.skycluster.io,resources=providersetups,verbs=get;list;watch;create;update;patch;delete

func (r *SkyXRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("ReconcilingSkyXRD %s %s", req.Name, req.NamespacedName))

	// Fetch the object
	providerSetupObj, err := r.GetUnstructuredResource(ctx, "ProviderSetup", "xrds.skycluster.io", "v1alpha1", req.Name, req.Namespace)
	if err != nil {
		logger.Error(err, "unable to fetch object, maybe it is deleted?")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if providerSetupObj != nil {
		logger.Info(fmt.Sprintf("ProviderSetup %s %s", providerSetupObj.GetName(), providerSetupObj.GetNamespace()))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyXRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gvk := schema.GroupVersionKind{
		Group:   "xrds.skycluster.io",
		Version: "v1alpha1",
		Kind:    "ProviderSetup",
	}
	unstructuredProviderSetupObj := &unstructured.Unstructured{}
	unstructuredProviderSetupObj.SetGroupVersionKind(gvk)

	return ctrl.NewControllerManagedBy(mgr).
		// For(&corev1alpha1.SkyXRD{}).
		Watches(
			unstructuredProviderSetupObj,
			&handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("core-skyxrd").
		Complete(r)
}

func (r *SkyXRDReconciler) GetUnstructuredResource(ctx context.Context, kind, group, version, reqName, reqNamescpae string) (*unstructured.Unstructured, error) {
	gvk := schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	}

	// Create an unstructured object
	unstructuredObj := &unstructured.Unstructured{}
	unstructuredObj.SetGroupVersionKind(gvk)
	newClientKey := client.ObjectKey{
		Name:      reqName,
		Namespace: reqNamescpae,
	}

	// Fetch the object using the client
	if err := r.Get(ctx, newClientKey, unstructuredObj); err != nil {
		// if !errors.IsNotFound(err) {
		// 	// Handle the error if it's not a NotFound error
		// 	return false, err
		// }
		return nil, err
	}
	return unstructuredObj, nil
}
