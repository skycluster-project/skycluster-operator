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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	policyv1alpha1 "github.com/etesami/skycluster-manager/api/policy/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeploymentPolicyReconciler reconciles a DeploymentPolicy object
type DeploymentPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies/finalizers,verbs=update

func (r *DeploymentPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	loggerName := "DPPolicy"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling DeploymentPolicy for [%s]", loggerName, req.Name))

	dpPolicy := &policyv1alpha1.DeploymentPolicy{}
	if err := r.Get(ctx, req.NamespacedName, dpPolicy); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	dpPolicy.SetCondition("Synced", metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")

	// Append the name of the DeploymentPolciy to the skycluster.core.skycluster.io/v1alpha1 object
	// If the SkyCluster object does not exist, create it and then append the name

	skyCluster := &corev1alpha1.SkyCluster{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: dpPolicy.Namespace,
		Name:      dpPolicy.Name,
	}, skyCluster); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t SkyCluster not found. trying to create it.", loggerName))
		skyCluster = &corev1alpha1.SkyCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpPolicy.Name,
				Namespace: dpPolicy.Namespace,
			},
			Spec: corev1alpha1.SkyClusterSpec{
				DeploymentPolciyRef: corev1.LocalObjectReference{
					Name: dpPolicy.Name,
				},
			},
		}
		// Set the DataflowPolicy as the owner of the SkyCluster
		if err := ctrl.SetControllerReference(dpPolicy, skyCluster, r.Scheme); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t Failed to set controller reference.", loggerName))
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, skyCluster); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t Failed to create SkyCluster.", loggerName))
			// We requeue the request to update the SkyCluster object
			// This may happen because DFPolicy controller may create the SkyCluster object at the same time
			return ctrl.Result{Requeue: true}, client.IgnoreAlreadyExists(err)
		}

		dpPolicy.SetCondition("Ready", metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")
		if err := r.Status().Update(ctx, dpPolicy); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t Failed to update DeploymentPolicy status.", loggerName))
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// The SkyCluster object exists, update it by appending the name of the DataflowPolicy
	if skyCluster.Spec.DeploymentPolciyRef.Name == "" {
		logger.Info(fmt.Sprintf("[%s]\t Updating SkyCluster.", loggerName))
		skyCluster.Spec.DeploymentPolciyRef.Name = dpPolicy.Name
		if err := r.Update(ctx, skyCluster); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t Failed to update SkyCluster.", loggerName))
			return ctrl.Result{}, err
		}
	}
	dpPolicy.SetCondition("Ready", metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")
	if err := r.Status().Update(ctx, dpPolicy); err != nil {
		logger.Error(err, fmt.Sprintf("[%s]\t Failed to update DeploymentPolicy status.", loggerName))
		return ctrl.Result{}, err
	}
	logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy reconciled successfully.", loggerName))

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeploymentPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.DeploymentPolicy{}).
		Named("policy-deploymentpolicy").
		Complete(r)
}
