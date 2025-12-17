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

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	policyv1alpha1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
)

// DataflowPolicyReconciler reconciles a DataflowPolicy object
type DataflowPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies/finalizers,verbs=update

func (r *DataflowPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciling DataflowPolicy started")

	df := &policyv1alpha1.DataflowPolicy{}
	if err := r.Get(ctx, req.NamespacedName, df); err != nil {
		r.Logger.Info("DataflowPolicy not found.")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	key := client.ObjectKey{Namespace: df.Namespace, Name: df.Name}
	ilp := &cv1a1.ILPTask{}
	if err := r.Get(ctx, key, ilp); err != nil {
		if client.IgnoreNotFound(err) == nil {
			newILP := &cv1a1.ILPTask{
				ObjectMeta: metav1.ObjectMeta{Name: df.Name, Namespace: df.Namespace},
				Spec: cv1a1.ILPTaskSpec{
					DataflowPolicyRef: cv1a1.DataflowPolicyRef{
						LocalObjectReference:    corev1.LocalObjectReference{Name: df.Name},
						DataflowResourceVersion: df.GetResourceVersion(), // to trigger ILPTask reconciliation
					},
				},
			}
			if err := ctrl.SetControllerReference(df, newILP, r.Scheme); err != nil {
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
		// make sure ILPTask references the correct DataflowPolicy
		if err := r.updateILPTaskRef(ctx, ilp, df.Name, df.GetResourceVersion()); err != nil {
			r.Logger.Error(err, "Failed to update ILPTask's DataflowPolicyRef.")
			return ctrl.Result{}, err
		}
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		r.Logger.Info("Conflict detected, retrying...")
		curILP := &cv1a1.ILPTask{}
		if err := r.Get(ctx, req.NamespacedName, curILP); err != nil {
			return err
		}
		return r.updateILPTaskRef(ctx, curILP, df.Name, df.GetResourceVersion())
	}); err != nil {
		r.Logger.Error(err, "Failed to update DataflowPolicy status.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DataflowPolicyReconciler) updateILPTaskRef(ctx context.Context, ilp *cv1a1.ILPTask, name string, ver string) error {
	orig := ilp.DeepCopy()
	ilp.Spec.DataflowPolicyRef = cv1a1.DataflowPolicyRef{
		LocalObjectReference:    corev1.LocalObjectReference{Name: name},
		DataflowResourceVersion: ver,
	}
	if err := r.Patch(ctx, ilp, client.MergeFrom(orig)); err != nil {
		if apierrors.IsConflict(err) {return nil}
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataflowPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.DataflowPolicy{}).
		Named("policy-dataflowpolicy").
		Complete(r)
}
