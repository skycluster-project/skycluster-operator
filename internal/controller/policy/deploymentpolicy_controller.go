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

package policy

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	policyv1alpha1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
)

// DeploymentPolicyReconciler reconciles a DeploymentPolicy object
type DeploymentPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies/finalizers,verbs=update

func (r *DeploymentPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciling DeploymentPolicy started")

	dp := &policyv1alpha1.DeploymentPolicy{}
	if err := r.Get(ctx, req.NamespacedName, dp); err != nil {
		r.Logger.Info("DeploymentPolicy not found.")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	key := client.ObjectKey{Namespace: dp.Namespace, Name: dp.Name}
	ilp := &corev1alpha1.ILPTask{}
	if err := r.Get(ctx, key, ilp); err != nil {
		if client.IgnoreNotFound(err) == nil {
			newILP := &corev1alpha1.ILPTask{
				ObjectMeta: metav1.ObjectMeta{Name: dp.Name, Namespace: dp.Namespace},
				Spec:       corev1alpha1.ILPTaskSpec{
					DeploymentPlanRef: corev1alpha1.DeploymentPlanRef{
						LocalObjectReference: corev1.LocalObjectReference{Name: dp.Name},
						DeploymentPlanResourceVersion: dp.GetResourceVersion(), // to trigger ILPTask reconciliation
					},
				},
			}
			if err := ctrl.SetControllerReference(dp, newILP, r.Scheme); err != nil {
				r.Logger.Error(err, "Failed to set owner reference.")
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, newILP); err != nil {
				if apierrors.IsAlreadyExists(err) {
					r.Logger.Info("ILPTask created concurrently, requeueing.")
					return ctrl.Result{Requeue: true}, nil
				}
				r.Logger.Error(err, "Failed to create ILPTask.")
				return ctrl.Result{}, err
			}
		} else {
			r.Logger.Error(err, "Failed to get ILPTask.")
			return ctrl.Result{}, err
		}
	} else {
		// make sure ILPTask references the correct DeploymentPolicy
		if err := r.updateILPTaskRef(ctx, ilp, dp.Name, dp.GetResourceVersion()); err != nil {
			r.Logger.Error(err, "Failed to update ILPTask's DeploymentPlanRef.")
			return ctrl.Result{}, err
		}
	}


	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cur := &policyv1alpha1.DeploymentPolicy{}
		if err := r.Get(ctx, req.NamespacedName, cur); err != nil {
			return err
		}
		cur.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")
		return r.Status().Update(ctx, cur)
	}); err != nil {
		r.Logger.Error(err, "Failed to update DeploymentPolicy status.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeploymentPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.DeploymentPolicy{}).
		Named("policy-deploymentpolicy").
		Complete(r)
}

func (r *DeploymentPolicyReconciler) updateILPTaskRef(ctx context.Context, ilp *corev1alpha1.ILPTask, name string, ver string) error {
	orig := ilp.DeepCopy()
	ilp.Spec.DeploymentPlanRef = corev1alpha1.DeploymentPlanRef{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		DeploymentPlanResourceVersion: ver,
	}
	if err := r.Patch(ctx, ilp, client.MergeFrom(orig)); err != nil {
		if apierrors.IsConflict(err) {return nil}
		return err
	}
	return nil
}