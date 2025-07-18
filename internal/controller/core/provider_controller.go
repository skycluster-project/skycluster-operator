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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// "github.com/aws/aws-sdk-go-v2/aws"
	// "github.com/aws/aws-sdk-go-v2/config"
	// "github.com/aws/aws-sdk-go-v2/credentials"
	// ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	// ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	// pricing "github.com/aws/aws-sdk-go-v2/service/pricing"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	helper "github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

// ProviderReconciler reconciles a Provider object
type ProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=providers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=providers/finalizers,verbs=update

func (r *ProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// logger := log.FromContext(ctx)
	logger := zap.New(helper.CustomLogger()).WithName("[Provider]")
	logger.Info(fmt.Sprintf("Reconciler started for %s", req.Name))

	// Copy all values from spec to status
	provider := &corev1alpha1.Provider{}
	if err := r.Get(ctx, req.NamespacedName, provider); err != nil {
		logger.Error(err, "unable to fetch Provider")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Copy all spec values to status
	provider.Status.Enabled = provider.Spec.Enabled
	provider.Status.Zones = make([]corev1alpha1.ZoneSpec, len(provider.Spec.Zones))
	provider.Status.Zones = lo.Filter(provider.Spec.Zones, func(zone corev1alpha1.ZoneSpec, _ int) bool { return true })
	if err := r.Status().Update(ctx, provider); err != nil {
		logger.Error(err, "unable to update Provider status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Provider{}).
		Named("core-provider").
		Complete(r)
}
