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
	"encoding/json"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	svcv1a1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
)

// XKubeReconciler reconciles a XKube object
type XKubeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=svc.skycluster.io,resources=xkubes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=xkubes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=xkubes/finalizers,verbs=update
// allow creating/updating the policy objects we create
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies/status,verbs=get;update;patch

func (r *XKubeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciling XKube", "namespace", req.Namespace, "name", req.Name)

	xk := &svcv1a1.XKube{}
	if err := r.Get(ctx, req.NamespacedName, xk); err != nil {
		// Not found -> nothing to do
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If being deleted, nothing to do here (ownerrefs will handle cleanup)
	if !xk.DeletionTimestamp.IsZero() {
		r.Logger.Info("XKube is being deleted, skipping policy creation", "name", xk.Name)
		return ctrl.Result{}, nil
	}

	// Use the XKube name as the app-id so both policies share same label and ILPTask logic can match them.
	appID := xk.Spec.ApplicationID

	// Create / ensure DeploymentPolicy exists
	if err := r.ensureDeploymentPolicy(ctx, xk, xk.Name, appID); err != nil {
		r.Logger.Error(err, "failed to ensure DeploymentPolicy", "name", xk.Name)
		return ctrl.Result{}, err
	}

	// Create / ensure DataflowPolicy exists
	if err := r.ensureDataflowPolicy(ctx, xk, xk.Name, appID); err != nil {
		r.Logger.Error(err, "failed to ensure DataflowPolicy", "name", xk.Name)
		return ctrl.Result{}, err
	}

	// Nothing else to do in this reconciler. ILP controllers (policy controllers) will pick up those objects
	// and create / update the ILPTask which triggers optimization.
	return ctrl.Result{}, nil
}

// ensureDeploymentPolicy ensures a DeploymentPolicy exists for the XKube.
// We create a multi-component deployment policy where each node group becomes a component.
func (r *XKubeReconciler) ensureDeploymentPolicy(ctx context.Context, owner *svcv1a1.XKube, name, appID string) error {
	// Try to get existing
	existing := &pv1a1.DeploymentPolicy{}
	key := client.ObjectKey{Namespace: owner.Namespace, Name: name}
	if err := r.Get(ctx, key, existing); err == nil {
		// Ensure label is present and ownerref exists; if not, update.
		needsPatch := false
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		if existing.Labels["skycluster.io/app-id"] != appID {
			existing.Labels["skycluster.io/app-id"] = appID
			needsPatch = true
		}
		// ensure owner reference
		if len(existing.OwnerReferences) == 0 {
			if err := ctrl.SetControllerReference(owner, existing, r.Scheme); err == nil {
				needsPatch = true
			}
		}
		if needsPatch {
			if err := r.Update(ctx, existing); err != nil { return err }
		}
		return nil
	} else if !apierrors.IsNotFound(err) {
		// unexpected error
		return err
	}

	// Not found -> create a DeploymentPolicy with one component per node group.
	dp := &pv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.Namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    appID,
				"skycluster.io/app-scope":  "distributed",
			},
		},
	}

	// Build DeploymentPolicyItems for each node group
	deploymentPolicies := []pv1a1.DeploymentPolicyItem{}
	for i, nodeGroup := range owner.Spec.NodeGroups {
		// Create VirtualServiceConstraints for each instance type in the node group
		virtualServiceConstraints := []pv1a1.VirtualServiceConstraint{}
		
		// Build AnyOf selectors for all instance types in this node group
		anyOfSelectors := []pv1a1.VirtualServiceSelector{}
		for _, instanceType := range nodeGroup.InstanceTypes {
			flavorJson, err := json.Marshal(instanceType)
			if err != nil { return err }
			
			r.Logger.Info("XKube node group instance type", "nodeGroup", i, "flavorSpec", string(flavorJson))

			anyOfSelectors = append(anyOfSelectors, pv1a1.VirtualServiceSelector{
				VirtualService: hv1a1.VirtualService{
					Kind: "ComputeProfile",
					Spec: &runtime.RawExtension{Raw: flavorJson},
				},
				Count: 1,
			})
		}

		// Create a single VirtualServiceConstraint with all instance types as AnyOf
		if len(anyOfSelectors) > 0 {
			virtualServiceConstraints = append(virtualServiceConstraints, pv1a1.VirtualServiceConstraint{
				AnyOf: anyOfSelectors,
			})
		}

		// Create LocationConstraint based on ProviderRef
		locationConstraint := hv1a1.LocationConstraint{
			Permitted: lo.Ternary(owner.Spec.ProviderRef.Platform != "", []hv1a1.ProviderRefSpec{
				owner.Spec.ProviderRef,
			}, []hv1a1.ProviderRefSpec{}),
		}

		// Create DeploymentPolicyItem for this node group
		// Each node group references the same XKube resource but represents a different component
		deploymentPolicies = append(deploymentPolicies, pv1a1.DeploymentPolicyItem{
			ComponentRef: hv1a1.ComponentRef{
				APIVersion: "svc.skycluster.io/v1alpha1",
				Kind:       "XKube",
				Name:       owner.Name,
				Namespace:  owner.Namespace, // namespace-scoped
			},
			VirtualServiceConstraint: virtualServiceConstraints,
			LocationConstraint:       locationConstraint,
		})
	}

	dp.Spec = pv1a1.DeploymentPolicySpec{
		ExecutionEnvironment: "Kubernetes",
		DeploymentPolicies:   deploymentPolicies,
	}

	// Set controller reference to cascade deletion from XKube
	if err := ctrl.SetControllerReference(owner, dp, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, dp); err != nil {
		// If already exists concurrently, that's fine
		if apierrors.IsAlreadyExists(err) { return nil }
		return err
	}
	r.Logger.Info("Created DeploymentPolicy for XKube", "deploymentPolicy", name, "xkube", owner.Name)
	return nil
}

// ensureDataflowPolicy ensures a minimal DataflowPolicy exists for the XKube.
// For a multi-component app (multiple node groups), no data dependencies are created (empty list).
func (r *XKubeReconciler) ensureDataflowPolicy(ctx context.Context, owner *svcv1a1.XKube, name, appID string) error {
	// Try to get existing
	existing := &pv1a1.DataflowPolicy{}
	key := client.ObjectKey{Namespace: owner.Namespace, Name: name}
	if err := r.Get(ctx, key, existing); err == nil {
		// Ensure label is present and ownerref exists; if not, update.
		needsPatch := false
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		if existing.Labels["skycluster.io/app-id"] != appID {
			existing.Labels["skycluster.io/app-id"] = appID
			needsPatch = true
		}
		// ensure owner reference
		if len(existing.OwnerReferences) == 0 {
			if err := ctrl.SetControllerReference(owner, existing, r.Scheme); err == nil {
				needsPatch = true
			}
		}
		if needsPatch {
			if err := r.Update(ctx, existing); err != nil {
				return err
			}
		}
		return nil
	} else if !apierrors.IsNotFound(err) {
		// unexpected error
		return err
	}

	// Not found -> create a minimal DataflowPolicy.
	df := &pv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.Namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":   appID,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DataflowPolicySpec{
			// For a multi-component app we leave DataDependencies empty.
			DataDependencies: func () []pv1a1.DataDapendency {
				DataDependencies := []pv1a1.DataDapendency{}
				for _, nodeGroup := range owner.Spec.NodeGroups {
					DataDependencies = append(DataDependencies, pv1a1.DataDapendency{
						From: hv1a1.ComponentRef{
							APIVersion: "skycluster.io/v1alpha1",
							Kind:       "NoOp",
						},
						To: hv1a1.ComponentRef{
							APIVersion: "skycluster.io/v1alpha1",
							Kind:       "NoOp",
						},
						TotalDataTransfer: nodeGroup.OutboundTraffic.TotalDataTransfer,
						AverageDataRate:   nodeGroup.OutboundTraffic.AverageDataRate,
					})
				}
				return DataDependencies
			}(),
		},
	}

	if err := ctrl.SetControllerReference(owner, df, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, df); err != nil {
		if apierrors.IsAlreadyExists(err) { return nil }
		return err
	}
	r.Logger.Info("Created DataflowPolicy for XKube", "dataflowPolicy", name, "xkube", owner.Name)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *XKubeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&svcv1a1.XKube{}).
		Named("svc-xkube").
		Complete(r)
}
