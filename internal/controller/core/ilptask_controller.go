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

/*
The ILPTask controller is responsible for running the optimization process.

The optimization process is run by creating a pod and mounting optimization scripts
as well as providers and tasks files. The providers files (.json) are generated during
the installation phase and stored in a persistent volume, however, the deployments (.json)
are generated in init container of the optimization pod. The csv files are generated within
the main container of the optimization pod.

These make the optimization process to be run without any external source of data
or hints. However, we introduce Sky Services such as SkyVM, and upon creation of such
services, we include them in the optimization process.

*/

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	policyv1alpha1 "github.com/etesami/skycluster-manager/api/policy/v1alpha1"
)

// ILPTaskReconciler reconciles a ILPTask object
type ILPTaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var OPTMIZATION_POD_NAME = "optimization-solver"

// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/finalizers,verbs=update

func (r *ILPTaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	loggerName := "ILPTask"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling %s", loggerName, req.Name))

	// Fetch the ILPTask instance
	instance := &corev1alpha1.ILPTask{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Failed to get ILPTask", loggerName))
		return ctrl.Result{}, err
	}

	// Waiting for pod to be completed
	// If the pod is completed, then update the result

	// If the result is already set, return and do nothing
	if instance.Status.Optimization.Result != "" { // Success or Imfeasible
		// Normally we don't re run the optimization, if the result is already set
		logger.Info(fmt.Sprintf("[%s]\t ILPTask already processed. Skipping the optimization.", loggerName))
		return ctrl.Result{}, nil
	}

	// The previous status is running
	if instance.Status.Optimization.Status == "Running" {
		// check the pod status, if it is done running, then update the result
		// if the pod is not done, then return requing the ILPTask
		podStatus, optResult, optDeployPlan, err := getPodStatusAndResult(ctx, r, req)
		if err != nil {
			logger.Info(fmt.Sprintf("[%s]\t Failed to get Pod status", loggerName))
			return ctrl.Result{}, err
		}
		if podStatus == "Succeeded" {
			// The optimization result may be Optimal or Infeasible
			instance.Status.Optimization.Result = optResult
			instance.Status.Optimization.Status = podStatus
			instance.Status.Optimization.ConfigMapRef = corev1.LocalObjectReference{
				Name: OPTMIZATION_POD_NAME,
			}
			instance.Status.Optimization.PodRef = corev1.LocalObjectReference{
				Name: OPTMIZATION_POD_NAME,
			}
			if err = json.Unmarshal([]byte(optDeployPlan), &instance.Status.Optimization.DeployMap); err != nil {
				logger.Info(fmt.Sprintf("[%s]\t Failed to unmarshal deploy plan", loggerName))
				return ctrl.Result{}, err
			}

			if err := r.Status().Update(ctx, instance); err != nil {
				logger.Info(fmt.Sprintf("[%s]\t Failed to update ILPTask status", loggerName))
				return ctrl.Result{}, err
			}
			logger.Info(fmt.Sprintf("[%s]\t ILPTask completed successfully.", loggerName))
			return ctrl.Result{}, nil
		}
		if podStatus == "Running" {
			logger.Info(fmt.Sprintf("[%s]\t Optimization pod not ready yet. Requeue...", loggerName))
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		}
		// If the pod is not succeeded or running, then it is failed
		instance.Status.Optimization.Status = podStatus
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Info(fmt.Sprintf("[%s]\t Failed to update ILPTask status", loggerName))
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("[%s]\t ILPTask failed. Check pod status.", loggerName))
		return ctrl.Result{}, nil
	}

	// If the status is not running, then it means the optimization was run previously
	// or it is the first time we are running the optimization, so we need to check the
	// status of the optimization (ILPTask.Status.Optimization.Result),
	// If it is empty, then we need to run the optimization
	if instance.Status.Optimization.Result != "" {
		logger.Info(fmt.Sprintf("[%s]\t ILPTask already processed. Skipping the optimization.", loggerName))
		return ctrl.Result{}, nil
	}

	// We need to schedule the optimization
	// Creating task definitions and task attribute files
	// Get all deployments with skycluster label
	// List all deployments that have skycluster labels
	allDeploy := &appsv1.DeploymentList{}
	if err := r.List(ctx, allDeploy, client.MatchingLabels{
		corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL: corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
	}); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error listing SkyApps.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info(fmt.Sprintf("[%s]\t Deployments [%d] found.", loggerName, len(allDeploy.Items)))

	// Get dataflow and deployment policies as we need them later
	dfPolicy := &policyv1alpha1.DataflowPolicy{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, dfPolicy); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t DataflowPolicy not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info(fmt.Sprintf("[%s]\t DataflowPolicy found.", loggerName))

	// If deployment contains skycluster annotations then create task definition and task attribute files
	// By having all files we are ready to run optimization
	// The optimization is done by creating a pod, and mounting optimization scripts
	// as well as providers and tasks files
	// The providers files (.json) are generated during the installation phase and stored
	// in a persistent volume, however, the deployments (.json) are generated in init container
	// of the optimization pod. The csv files are generated within the main container of the
	// optimization pod.
	// Provider files (generated during the setup)
	//    '/shared/providers.json',
	//    '/shared/providers.csv',
	//    '/shared/providers-attr.json',
	//    '/shared/providers-attr.csv',
	//    '/shared/offerings.json',
	//    '/shared/vservices.csv',
	// Deployment files (init container)
	//    '/shared/deployments.json',
	//    '/shared/deployments-attr.json'
	// CSV files (main container, before running the optimization)
	//    '/shared/tasks.json'
	//    '/shared/tasksEdges.json'
	//    '/shared/tasks-locations.json'
	// Results
	//    '/shared/optimization-stats.csv'
	//    '/shared/deploy-plan.json'
	// The result of the optimization is stored in the deploy-plan.json file and
	// a configmap is created to store the results with label
	// skycluster.io/config-type: optimization-status
	// First we retrive the confimap and then use it within the pod definition

	// Get ConfigMap by label
	var configMapList corev1.ConfigMapList
	if err := r.List(ctx, &configMapList, client.MatchingLabels{
		corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL:  corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
		corev1alpha1.SKYCLUSTER_CONFIGTYPE_LABEL: "optimization-starter",
	}); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error listing ConfigMaps (optimization starter).", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// we expect to have only one configmap
	if len(configMapList.Items) != 1 {
		logger.Info(fmt.Sprintf("[%s]\t Multiple configmaps exist (optimization-starter)", loggerName))
		return ctrl.Result{}, nil
	}

	// Define Pod
	pod := defineOptimizationPod(ctx, r, configMapList)
	if err := r.Create(ctx, pod); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Failed to create Pod", loggerName))
		return ctrl.Result{}, err
	}

	// We need to requeue the ILPTask to check the status of the optimization
	instance.Status.Optimization.Status = "Running"
	if err := r.Status().Update(ctx, instance); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Failed to update ILPTask status", loggerName))
		return ctrl.Result{}, err
	}
	logger.Info(fmt.Sprintf("[%s]\t ILPTask scheduled for optimization.", loggerName))
	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

func defineOptimizationPod(ctx context.Context, r *ILPTaskReconciler, configMapList corev1.ConfigMapList) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OPTMIZATION_POD_NAME,
			Namespace: corev1alpha1.SKYCLUSTER_NAMESPACE,
			Labels: map[string]string{
				corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL: corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "skycluster-sva",
			InitContainers: []corev1.Container{
				{
					Name: "kubectl",
					// TODO: the image registry should be configurable
					Image: "registry.skycluster.io/kubectl:latest",
					Command: []string{
						"/bin/sh",
						"-c",
					},
					Args: []string{
						configMapList.Items[0].Data["init.sh"],
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/shared",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "ubuntu-python",
					Image: "registry.skycluster.io/ubuntu-python:3.10",
					Command: []string{
						"/bin/sh",
						"-c",
					},
					Args: []string{
						strings.ReplaceAll(
							configMapList.Items[0].Data["main.sh"],
							"__CONFIG_NAME__",
							OPTMIZATION_POD_NAME),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/shared",
						},
						{
							Name:      "scripts",
							MountPath: "/scripts",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "scripts",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: configMapList.Items[0].Name,
							},
						},
					},
				},
				{
					Name: "shared",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "skycluster-pvc",
						},
					},
				},
			},
		},
	}
	return pod
}

// Returns:
// - podStatus: The current status of the pod.
// - optimizationStatus: The status of the optimization process.
// - deploymentPlan: The deployment plan details.
// - error: An error object if an error occurred, otherwise nil.
func getPodStatusAndResult(ctx context.Context, r *ILPTaskReconciler, req ctrl.Request) (podStatus string, optmizationStatus string, deployPlan string, err error) {
	pod := &corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: corev1alpha1.SKYCLUSTER_NAMESPACE,
		Name:      OPTMIZATION_POD_NAME,
	}, pod); err != nil {
		return "", "", "", err
	}
	if pod.Status.Phase == corev1.PodSucceeded {
		// get the result from the configmap
		configMap := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: corev1alpha1.SKYCLUSTER_NAMESPACE,
			Name:      OPTMIZATION_POD_NAME,
		}, configMap); err != nil {
			return "", "", "", err
		}
		// The result of the optimization could be Optimal or Infeasible
		return string(pod.Status.Phase), configMap.Data["result"], configMap.Data["deploy-plan"], nil
	}
	// When the pod is not completed yet or not succeeded
	// there is no result to return except the pod status
	return string(pod.Status.Phase), "", "", nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ILPTaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.ILPTask{}).
		Named("core-ilptask").
		Complete(r)
}
