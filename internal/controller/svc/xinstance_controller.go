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
	policyv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	svcv1a1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
)

// XInstanceReconciler reconciles a XInstance object
type XInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=svc.skycluster.io,resources=xinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=xinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=xinstances/finalizers,verbs=update
// allow creating/updating the policy objects we create
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=deploymentpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.skycluster.io,resources=dataflowpolicies/status,verbs=get;update;patch

func (r *XInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciling XInstance", "namespace", req.Namespace, "name", req.Name)

	xi := &svcv1a1.XInstance{}
	if err := r.Get(ctx, req.NamespacedName, xi); err != nil {
		// Not found -> nothing to do
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If being deleted, nothing to do here (ownerrefs will handle cleanup)
	if !xi.DeletionTimestamp.IsZero() {
		r.Logger.Info("XInstance is being deleted, skipping policy creation", "name", xi.Name)
		return ctrl.Result{}, nil
	}

	// Use the XInstance name as the app-id so both policies share same label and ILPTask logic can match them.
	appID := xi.Spec.ApplicationID

	// Create / ensure DeploymentPolicy exists
	if err := r.ensureDeploymentPolicy(ctx, xi, xi.Name, appID); err != nil {
		r.Logger.Error(err, "failed to ensure DeploymentPolicy", "name", xi.Name)
		return ctrl.Result{}, err
	}

	// Create / ensure DataflowPolicy exists
	if err := r.ensureDataflowPolicy(ctx, xi, xi.Name, appID); err != nil {
		r.Logger.Error(err, "failed to ensure DataflowPolicy", "name", xi.Name)
		return ctrl.Result{}, err
	}

	// Nothing else to do in this reconciler. ILP controllers (policy controllers) will pick up those objects
	// and create / update the ILPTask which triggers optimization.
	return ctrl.Result{}, nil
}

// ensureDeploymentPolicy ensures a minimal DeploymentPolicy exists for the XInstance.
// We create a single-component deployment policy that references a Deployment with the same name
// as the XInstance. We intentionally keep the LocationConstraint permissive (empty provider refs)
// so the ILP optimizer can find candidate providers.
func (r *XInstanceReconciler) ensureDeploymentPolicy(ctx context.Context, owner *svcv1a1.XInstance, name, appID string) error {
	// Try to get existing
	existing := &policyv1a1.DeploymentPolicy{}
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

	// Not found -> create a minimal DeploymentPolicy.
	dp := &policyv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.Namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    appID,
				"skycluster.io/app-scope": "distributed",
			},
		},
	}

	flavorJson, err := json.Marshal(hv1a1.ComputeFlavor{
		VCPUs: owner.Spec.Flavor.VCPUs,
		RAM:   owner.Spec.Flavor.RAM,
		GPU: hv1a1.GPU{
			Model:  owner.Spec.Flavor.GPU.Model,
			Unit:   owner.Spec.Flavor.GPU.Unit,
			Memory: owner.Spec.Flavor.GPU.Memory,
		},
	})
	if err != nil {return err}
	r.Logger.Info("XInstance flavor spec", "flavorSpec", string(flavorJson))

	// Build a minimal DeploymentPolicySpec for a single component.
	// The component reference points to a Deployment with the same name as the XInstance.
	// VirtualServiceConstraint contains a ManagedKubernetes alternative so the optimizer
	// can consider managed k8s offerings (this matches ILPTask logic).
	dp.Spec = policyv1a1.DeploymentPolicySpec{
		ExecutionEnvironment: "VirtualMachine",
		DeploymentPolicies: []policyv1a1.DeploymentPolicyItem{
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "skycluster.io/v1alpha1",
					Kind:       "XInstance",
					Name:       owner.Name,
					// Namespace:  "", // cluster-scoped
				},
				// a single VirtualServiceConstraint whose AnyOf contains ManagedKubernetes
				VirtualServiceConstraint: []policyv1a1.VirtualServiceConstraint{
					{
						AnyOf: []policyv1a1.VirtualServiceSelector{
							{
								// VirtualServiceSelector embeds hv1a1.VirtualService inline.
								VirtualService: hv1a1.VirtualService{
									Kind: "ComputeProfile",
									Spec: &runtime.RawExtension{Raw: flavorJson},
								},
								Count: 1,
							},
						},
					},
				},
				// LocationConstraint: permissive (no specific provider filters) to allow optimizer to choose
				LocationConstraint: hv1a1.LocationConstraint{
					Permitted: lo.Ternary(owner.Spec.ProviderRef.Platform != "", []hv1a1.ProviderRefSpec{
							owner.Spec.ProviderRef,
						}, []hv1a1.ProviderRefSpec{}),
				},
			},
		},
	}

	// Set controller reference to cascade deletion from XInstance
	if err := ctrl.SetControllerReference(owner, dp, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, dp); err != nil {
		// If already exists concurrently, that's fine
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	r.Logger.Info("Created DeploymentPolicy for XInstance", "deploymentPolicy", name, "xinstance", owner.Name)
	return nil
}

// ensureDataflowPolicy ensures a minimal DataflowPolicy exists for the XInstance.
// For a single-component app, no data dependencies are created (empty list).
func (r *XInstanceReconciler) ensureDataflowPolicy(ctx context.Context, owner *svcv1a1.XInstance, name, appID string) error {
	// Try to get existing
	existing := &policyv1a1.DataflowPolicy{}
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
	df := &policyv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.Namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    appID,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: policyv1a1.DataflowPolicySpec{
			// For a single component app we leave DataDependencies empty.
			DataDependencies: []policyv1a1.DataDapendency{},
		},
	}

	if err := ctrl.SetControllerReference(owner, df, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, df); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	r.Logger.Info("Created DataflowPolicy for XInstance", "dataflowPolicy", name, "xinstance", owner.Name)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *XInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&svcv1a1.XInstance{}).
		Named("svc-xinstance").
		Complete(r)
}
