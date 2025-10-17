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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	policyv1alpha1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
)

// DataflowPolicyReconciler reconciles a DataflowPolicy object
type DataflowPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies/finalizers,verbs=update

func (r *DataflowPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger, ln := log.FromContext(ctx), "DFPolicy"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling DataflowPolicy for [%s]", ln, req.NamespacedName))

	df := &policyv1alpha1.DataflowPolicy{}
	if err := r.Get(ctx, req.NamespacedName, df); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t DataflowPolicy not found.", ln))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	key := client.ObjectKey{Namespace: df.Namespace, Name: df.Name}
	ilp := &corev1alpha1.ILPTask{}
	if err := r.Get(ctx, key, ilp); err != nil {
		if client.IgnoreNotFound(err) == nil {
			newILP := &corev1alpha1.ILPTask{
				ObjectMeta: metav1.ObjectMeta{Name: df.Name, Namespace: df.Namespace},
				Spec:       corev1alpha1.ILPTaskSpec{DataflowPolicyRef: corev1.LocalObjectReference{Name: df.Name}},
			}
			if err := ctrl.SetControllerReference(df, newILP, r.Scheme); err != nil {
				logger.Error(err, fmt.Sprintf("[%s]\t Failed to set owner reference.", ln))
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, newILP); err != nil {
				if apierrors.IsAlreadyExists(err) {
					logger.Info(fmt.Sprintf("[%s]\t ILPTask created concurrently, requeueing.", ln))
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, fmt.Sprintf("[%s]\t Failed to create ILPTask.", ln))
				return ctrl.Result{}, err
			}
		} else {
			logger.Error(err, fmt.Sprintf("[%s]\t Failed to get ILPTask.", ln))
			return ctrl.Result{}, err
		}
	} else if ilp.Spec.DataflowPolicyRef.Name == "" {
		orig := ilp.DeepCopy()
		ilp.Spec.DataflowPolicyRef.Name = df.Name
		if err := r.Patch(ctx, ilp, client.MergeFrom(orig)); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, fmt.Sprintf("[%s]\t Failed to update ILPTask.", ln))
			return ctrl.Result{}, err
		}
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cur := &policyv1alpha1.DataflowPolicy{}
		if err := r.Get(ctx, req.NamespacedName, cur); err != nil {
			return err
		}
		cur.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")
		return r.Status().Update(ctx, cur)
	}); err != nil {
		logger.Error(err, fmt.Sprintf("[%s]\t Failed to update DataflowPolicy status.", ln))
		return ctrl.Result{}, err
	}

	logger.Info(fmt.Sprintf("[%s]\t Reconciled DataflowPolicy for [%s]", ln, req.NamespacedName))
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataflowPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.DataflowPolicy{}).
		Named("policy-dataflowpolicy").
		Complete(r)
}
