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
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	// "time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller/utils"
)

// ILPTaskReconciler reconciles a ILPTask object
type ILPTaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=latencies,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/finalizers,verbs=update

func (r *ILPTaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciling ILPTask started", "namespace", req.Namespace, "name", req.Name)

	// Fetch the ILPTask instance
	task := &cv1a1.ILPTask{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		r.Logger.Info("unable to fetch ILPTask, cleaning up optimization pod if exists", "name", req.Name)
		_ = r.removeOptimizationPod(ctx, req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// if deleted, clean up optimization pod
	if !task.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Logger.Info("ILPTask is being deleted; cleaning up optimization pod", "podName", task.Status.Optimization.PodRef.Name)
		_ = r.removeOptimizationPod(ctx, task.Name)
		return ctrl.Result{}, nil
	}

	dfName := task.Spec.DataflowPolicyRef.Name
	dpName := task.Spec.DeploymentPolicyRef.Name
	if dfName == "" || dpName == "" {
		r.Logger.Info("ILPTask references are incomplete; skipping optimization", "DataflowPolicyRef", dfName, "DeploymentPlanRef", dpName)
		return ctrl.Result{}, nil
	}

	status := task.Status.Optimization

	// Fetch current resourceVersions of referenced DataflowPolicy and DeploymentPlan.
	df := pv1a1.DataflowPolicy{}
	if err := r.Get(ctx, req.NamespacedName, &df); err != nil {
		r.Logger.Error(err, "unable to fetch DataflowPolicy", "name", dfName)
		// requeue on error
		return ctrl.Result{}, err
	}

	dp := pv1a1.DeploymentPolicy{}
	if err := r.Get(ctx, req.NamespacedName, &dp); err != nil {
		r.Logger.Error(err, "unable to fetch DeploymentPlan", "name", dpName)
		return ctrl.Result{}, err
	}

	appId1, ok1 := df.Labels["skycluster.io/app-id"]
	appId2, ok2 := dp.Labels["skycluster.io/app-id"]
	if !ok1 || !ok2 || appId1 != appId2 {
		return ctrl.Result{}, errors.New("objects DataflowPolicy and DeploymentPolicy must have the same skycluster.io/app-id label")
	}

	currDFRV := df.ObjectMeta.ResourceVersion
	currDPRV := dp.ObjectMeta.ResourceVersion

	// If result is already set and referenced resources didn't change -> skip
	if status.Result != "" && status.DataflowResourceVersion == currDFRV && status.DeploymentPlanResourceVersion == currDPRV {
		r.Logger.Info("ILPTask already processed and references unchanged. Skipping optimization.")
		// generate skyXRD object
		if err := r.ensureAtlas(task, appId1, dp.Spec.ExecutionEnvironment, status.DeployMap); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to ensure Atlas after optimization")
		}
		if err := r.ensureAtlasMesh(task, appId1, status.DeployMap); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to ensure AtlasMesh after optimization")
		}
		return ctrl.Result{}, nil
	}

	// Otherwise create or ensure optimization pod
	podName := task.Status.Optimization.PodRef.Name

	if podName == "" { // not yet created
		// Pod not found -> create one to run optimization
		r.Logger.Info("Creating optimization pod for ILPTask")
		_, err := r.prepareAndBuildOptimizationPod(df, dp, task)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil

	} else { // pod already created - check status

		r.Logger.Info("Fetching optimization pod for ILPTask", "podName", podName)
		pod := &corev1.Pod{}
		err := r.Get(ctx, types.NamespacedName{Namespace: hv1a1.SKYCLUSTER_NAMESPACE, Name: podName}, pod)

		if err == nil { // pod exists - check phase
			r.Logger.Info("Optimization pod found")
			if pod.Status.Phase == corev1.PodSucceeded {
				// Pod completed - read result from a ConfigMap or pod logs in real implementation.
				// For now, we set Result based on PodSucceeded/Failed.

				if pod.Status.Phase == corev1.PodSucceeded {
					optDeployPlan, err := r.getPodStatusAndResult(podName)
					if err != nil {
						return ctrl.Result{}, errors.Wrap(err, "failed to get pod status and result")
					}

					// if the result is optimal we set the deployment plan
					deployPlan := cv1a1.DeployMap{}
					if err = json.Unmarshal([]byte(optDeployPlan), &deployPlan); err != nil {
						return ctrl.Result{}, errors.Wrap(err, "failed to unmarshal deploy plan")
					}

					// Find and add required services (virtual services like ComputeProfile) to the deployPlan
					requiredServices, err := r.findServicesForDeployPlan(req.Namespace, deployPlan, dp)
					if err != nil {
						r.Logger.Error(err, "failed to find required services for deploy plan, continuing without them")
						// Continue without required services rather than failing
					} else {
						// Set manifest for existing components with required virtual services
						for componentName, services := range requiredServices {
							if len(services) == 0 {
								continue
							}

							// Find the existing component and update its manifest
							for i := range deployPlan.Component {
								if deployPlan.Component[i].ComponentRef.Name == componentName {

									manifest := make(map[string][]any)

									// supporting alternative virtual service sets
									for _, service := range services {
										// set the first 5 services as the manifest for each alternative
										topNServices := min(len(service), 5)
										manifest["services"] = append(manifest["services"], service[:topNServices])
									}

									manifestBytes, err := json.Marshal(manifest)
									if err != nil {
										r.Logger.Error(err, "failed to marshal compute profile manifest", "component", componentName)
										continue
									}

									// Set the manifest for the existing component
									deployPlan.Component[i].Manifest = &runtime.RawExtension{Raw: manifestBytes}
									break
								}
							}
						}
					}

					task.Status.Optimization.DeployMap = deployPlan
					// at least one component deployed needed.
					// TODO: check if at least one edge is deployed?
					if len(deployPlan.Component) > 0 {
						// If the optimization result is "Optimal" and status is "Succeeded",
						// We have the deployment plan and we can update the SkyCluster object. (trigger deployment)
						// skyCluster, err = r.updateSkyCluster(ctx, req, deployPlan, "Optimal", "Succeeded")
						task.Status.Optimization.Status = "Succeeded"
						task.Status.Optimization.Result = "Success"
						task.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "OptimizationSucceeded", "ILPTask optimization succeeded")

						// generate atlas object
						if err = r.ensureAtlas(task, appId1, dp.Spec.ExecutionEnvironment, deployPlan); err != nil {
							return ctrl.Result{}, errors.Wrap(err, "failed to ensure Atlas after optimization")
						}
						if err = r.ensureAtlasMesh(task, appId1, deployPlan); err != nil {
							return ctrl.Result{}, errors.Wrap(err, "failed to ensure AtlasMesh after optimization")
						}
					}
				} else {
					task.Status.Optimization.Status = "Failed"
					task.Status.Optimization.Result = "Infeasible"
					task.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "OptimizationFailed", "ILPTask optimization failed")
				}

				// update stored resource versions to current so we won't rerun unnecessarily
				task.Status.Optimization.DataflowResourceVersion = currDFRV
				task.Status.Optimization.DeploymentPlanResourceVersion = currDPRV

				if err := r.Status().Update(ctx, task); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "failed to update ILPTask status after pod completion")
				}
				r.Logger.Info("Optimization pod completed; status updated", "phase", pod.Status.Phase)
				return ctrl.Result{}, nil
			} else { // Pod still running
				r.Logger.Info("Optimization pod already running")
				return ctrl.Result{RequeueAfter: 3 * time.Second}, nil // requeue in 3s
			}
		} else { // error fetching pod
			// remove pod name
			task.Status.Optimization.PodRef = corev1.LocalObjectReference{}
			// Retry updating status on conflict
			updateErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				latest := &cv1a1.ILPTask{}
				if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Name}, latest); err != nil {
					return err
				}
				latest.Status.Optimization.PodRef = task.Status.Optimization.PodRef // copy prepared status
				return r.Status().Update(ctx, latest)
			})
			if updateErr != nil {
				r.Logger.Error(updateErr, "failed to update ILPTask status after pod completion")
			}
			return ctrl.Result{}, fmt.Errorf("failed to get optimization pod: %w", err)
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ILPTaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.ILPTask{}).
		Named("core-ilptask").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}

func newCustomRateLimiter() workqueue.TypedRateLimiter[reconcile.Request] {
	return workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Second, 30*time.Second),
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)
}

func (r *ILPTaskReconciler) ensureAtlasMesh(task *cv1a1.ILPTask, appId string, deployPlan cv1a1.DeployMap) error {
	obj := &cv1a1.AtlasMeshList{}
	if err := r.List(context.TODO(), obj, client.InNamespace(task.Namespace), client.MatchingLabels{
		"skycluster.io/app-id": appId,
	}); err != nil {
		return errors.Wrapf(err, "failed to list AtlasMeshs for ILPTask %s", task.Name)
	}
	if len(obj.Items) > 1 {
		return fmt.Errorf("multiple AtlasMeshs found for ILPTask %s and appId %s", task.Name, appId)
	}
	if len(obj.Items) > 0 {
		// already exists, update if necessary
		obj := &obj.Items[0]
		if !reflect.DeepEqual(obj.Spec.DeployMap, deployPlan) {
			r.Logger.Info("Updating existing AtlasMesh with new deployment plan", "AtlasMesh", obj.Name)
			obj.Spec.DeployMap = deployPlan
			obj.Spec.Approve = false
			if err := r.Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update AtlasMesh for ILPTask %s", task.Name)
			}
			obj.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "PendingApproval", "AtlasMesh pending approval")
			if err := r.Status().Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update AtlasMesh status for ILPTask %s", task.Name)
			}
		}
		return nil
	} else { // try to create
		r.Logger.Info("Creating new AtlasMesh for deployment plan", "ILPTask", task.Name)
		obj := &cv1a1.AtlasMesh{
			ObjectMeta: metav1.ObjectMeta{
				Name:      task.Name + "-" + utils.RandSuffix(task.Name),
				Labels:    map[string]string{"skycluster.io/app-id": appId},
				Namespace: task.Namespace,
			},
			Spec: cv1a1.AtlasMeshSpec{
				Approve:             false,
				DataflowPolicyRef:   task.Spec.DataflowPolicyRef,
				DeploymentPolicyRef: task.Spec.DeploymentPolicyRef,
				DeployMap:           deployPlan,
			},
		}
		// set owner reference to ILPTask
		if err := ctrl.SetControllerReference(task, obj, r.Scheme); err != nil {
			return errors.Wrapf(err, "failed to set owner reference for Atlas for ILPTask %s", task.Name)
		}
		if err := r.Create(context.TODO(), obj); err != nil {
			return errors.Wrapf(err, "failed to create Atlas for ILPTask %s", task.Name)
		}
	}

	return nil
}

func (r *ILPTaskReconciler) ensureAtlas(task *cv1a1.ILPTask, appId string, execEnv string, deployPlan cv1a1.DeployMap) error {
	obj := &cv1a1.AtlasList{}
	if err := r.List(context.TODO(), obj, client.InNamespace(task.Namespace), client.MatchingLabels{
		"skycluster.io/app-id": appId,
	}); err != nil {
		return errors.Wrapf(err, "failed to list Atlass for ILPTask %s", task.Name)
	}
	if len(obj.Items) > 1 {
		return fmt.Errorf("multiple Atlass found for ILPTask %s and appId %s", task.Name, appId)
	}
	if len(obj.Items) > 0 { // already exists, update if necessary
		obj := &obj.Items[0]
		if !reflect.DeepEqual(obj.Spec.DeployMap, deployPlan) {
			r.Logger.Info("Updating existing Atlas with new deployment plan", "Atlas", obj.Name)
			obj.Spec.DeployMap = deployPlan
			obj.Spec.Approve = false
			if err := r.Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update Atlas for ILPTask %s", task.Name)
			}
			obj.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "PendingApproval", "Atlas pending approval")
			if err := r.Status().Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update Atlas status for ILPTask %s", task.Name)
			}
		}
		return nil
	} else { // try to create
		r.Logger.Info("Creating new Atlas for deployment plan", "ILPTask", task.Name)
		obj := &cv1a1.Atlas{
			ObjectMeta: metav1.ObjectMeta{
				Name:      task.Name + "-" + utils.RandSuffix(task.Name),
				Labels:    map[string]string{"skycluster.io/app-id": appId},
				Namespace: task.Namespace,
			},
			Spec: cv1a1.AtlasSpec{
				Approve:              false,
				ExecutionEnvironment: execEnv,
				DataflowPolicyRef:    task.Spec.DataflowPolicyRef,
				DeploymentPolicyRef:  task.Spec.DeploymentPolicyRef,
				DeployMap:            deployPlan,
			},
		}
		// set owner reference to ILPTask
		if err := ctrl.SetControllerReference(task, obj, r.Scheme); err != nil {
			return errors.Wrapf(err, "failed to set owner reference for Atlas for ILPTask %s", task.Name)
		}
		if err := r.Create(context.TODO(), obj); err != nil {
			return errors.Wrapf(err, "failed to create Atlas for ILPTask %s", task.Name)
		}
	}

	return nil
}
