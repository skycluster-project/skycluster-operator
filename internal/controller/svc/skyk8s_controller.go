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

package svc

import (
	"context"
	"fmt"

	svcv1alpha1 "github.com/etesami/skycluster-manager/api/svc/v1alpha1"

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

// SkyK8SReconciler reconciles a SkyK8S object
type SkyK8SReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyk8s,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyk8s/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyk8s/finalizers,verbs=update

// We need to watch SkyK8SCluster object to learn about the ProviderConfigRef
// +kubebuilder:rbac:groups=xrds.skycluster.io,resources=skyk8sclusters,verbs=update;patch;delete

func (r *SkyK8SReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logName := "SkyK8S"
	logger.Info(fmt.Sprintf("[%s]\tReconciler started for %s", logName, req.Name))

	// Fetch the object (SkyK8SCluster)
	skyk8s := &unstructured.Unstructured{}
	skyk8s.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "xrds.skycluster.io",
		Version: "v1alpha1",
		Kind:    "SkyK8SCluster",
	})
	if err := r.Get(ctx, req.NamespacedName, skyk8s); err != nil {
		logger.Info(fmt.Sprintf("[%s]\tunable to fetch SkyK8SCluster. Not expected", logName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// we need to see if its ProviderConfigRef is set and if so, we update the SkyApp object.
	k3sCfg, err := GetNestedField(skyk8s.Object, "status", "k3s")
	if err != nil {
		logger.Info(fmt.Sprintf("[%s]\tunable to get k3s config", logName))
		return ctrl.Result{}, nil
		// return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	pCfgName := k3sCfg["providerConfig"].(string)
	if pCfgName != "" {
		skyApp := &svcv1alpha1.SkyApp{}
		if err := r.Get(ctx, client.ObjectKey{
			Name: req.Name, Namespace: req.Namespace,
		}, skyApp); err != nil {
			logger.Info(fmt.Sprintf("[%s]\tunable to fetch SkyApp", logName))
			// The SkyApp may not be created yet, so we ignore the error
			// and request a requeue.
			return ctrl.Result{Requeue: true}, client.IgnoreNotFound(err)
		}
		skyApp.Status.ProviderConfigRef = pCfgName
		if err := r.Status().Update(ctx, skyApp); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\tunable to update SkyApp", logName))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\tupdated SkyApp with ProviderConfigRef", logName))
		return ctrl.Result{}, nil
	}
	logger.Info(fmt.Sprintf("[%s]\tProviderConfigRef is not set (yet)", logName))
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyK8SReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gvk := schema.GroupVersionKind{
		Group:   "xrds.skycluster.io",
		Version: "v1alpha1",
		Kind:    "SkyK8SCluster",
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	return ctrl.NewControllerManagedBy(mgr).
		Named("svc-skyk8s").
		Watches(obj,
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Complete(r)
}
