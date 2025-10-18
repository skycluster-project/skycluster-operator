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

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

// SkyXRDReconciler reconciles a SkyXRD object
type SkyXRDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=core,resources=skyxrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=skyxrds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=skyxrds/finalizers,verbs=update

func (r *SkyXRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started")

	// Fetch the object
	skyxrd := &cv1a1.SkyXRD{}
	if err := r.Get(ctx, req.NamespacedName, skyxrd); err != nil {
		r.Logger.Info("SkyXRD not found.")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil

	// skyxrd.SetCondition("Synced", metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")

	// for _, xrd := range skyxrd.Spec.Manifests {
	// 	var obj map[string]any
	// 	if err := yaml.Unmarshal([]byte(xrd.Manifest), &obj); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\tunable to unmarshal object", logName))
	// 		r.setConditionUnreadyAndUpdate(skyxrd, "unable to unmarshal object")
	// 		return ctrl.Result{}, err
	// 	}

	// 	unstrObj := &unstructured.Unstructured{Object: obj}
	// 	unstrObj.SetAPIVersion(xrd.ComponentRef.APIVersion)
	// 	unstrObj.SetKind(xrd.ComponentRef.Kind)
	// 	// The name contains "." which is not allowed when working with xrds
	// 	// We keep the orignial name as the Name field for SkyService object
	// 	// and replace "." with "-" for the object name when we need to work with XRDs
	// 	unstrObj.SetName(strings.Replace(xrd.ComponentRef.Name, ".", "-", -1))
	// 	if err := ctrl.SetControllerReference(skyxrd, unstrObj, r.Scheme); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\tunable to set controller reference", logName))
	// 		r.setConditionUnreadyAndUpdate(skyxrd, "unable to set controller reference")
	// 		return ctrl.Result{}, err
	// 	}

	// 	// if err := r.Create(ctx, unstrObj); err != nil {
	// 	// 	logger.Info(fmt.Sprintf("[%s]\tunable to create object, maybe it already exists?", logName))
	// 	// 	return ctrl.Result{}, client.IgnoreAlreadyExists(err)
	// 	// }
	// 	logger.Info(fmt.Sprintf("[%s]\tcreated object [%s]", logName, xrd.ComponentRef.Name))
	// }

	// r.setConditionReadyAndUpdate(skyxrd)
	// return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyXRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.SkyXRD{}).
		Named("core-skyxrd").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}
