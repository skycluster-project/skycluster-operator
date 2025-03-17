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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	policyv1alpha1 "github.com/etesami/skycluster-manager/api/policy/v1alpha1"
	"github.com/pkg/errors"
)

// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/finalizers,verbs=update

// ILPTaskReconciler reconciles a ILPTask object
type ILPTaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var OPTMIZATION_POD_NAME = "optimization-solver"

func (r *ILPTaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	loggerName := "ILPTask"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling %s", loggerName, req.Name))

	// Fetch the ILPTask instance
	instance := &corev1alpha1.ILPTask{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Failed to get ILPTask, maybe it is deleted?", loggerName))
		logger.Info(fmt.Sprintf("[%s]\t Deleting the optimization pod as the ILPTask is not found.", loggerName))
		// Delete the optimization pod
		pod := &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: corev1alpha1.SKYCLUSTER_NAMESPACE,
			Name:      OPTMIZATION_POD_NAME,
		}, pod); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if err := r.Delete(ctx, pod); err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t Failed to delete Pod.", loggerName))
		}
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

	// The previous status is set, so a pod is created
	if instance.Status.Optimization.Status != "" {
		// check the pod status, if it is done running, then update the result
		// if the pod is not done, then return requing the ILPTask
		podStatus, optResult, optDeployPlan, err := getPodStatusAndResult(ctx, r)
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
			// We set the deployPlan only if the result is Optimal
			skyCluster := &corev1alpha1.SkyCluster{}
			if optResult == "Optimal" {
				deployPlan := corev1alpha1.DeployMap{}
				if err = json.Unmarshal([]byte(optDeployPlan), &deployPlan); err != nil {
					logger.Info(fmt.Sprintf("[%s]\t Failed to unmarshal deploy plan", loggerName))
					return ctrl.Result{}, err
				}
				instance.Status.Optimization.DeployMap = deployPlan

				// If the optimization result is "Optimal" and status is "Succeeded",
				// We have the deployment plan and we can update the SkyCluster object.
				skyCluster, err = r.updateSkyCluster(ctx, req, deployPlan, "Optimal", "Succeeded")
				if err != nil {
					logger.Info(fmt.Sprintf("[%s]\t Failed to get SkyCluster upon updating with ILPTask results.", loggerName))
					return ctrl.Result{}, err
				}
			}
			// We now update the ILPTask status
			if err := r.Status().Update(ctx, instance); err != nil {
				logger.Info(fmt.Sprintf("[%s]\t Failed to update ILPTask status", loggerName))
				return ctrl.Result{}, err
			}
			logger.Info(fmt.Sprintf("[%s]\t ILPTask completed successfully.", loggerName))

			// We update the SkyCluster object only if the optimization result is "Optimal"
			// Since I want the update to happen only after the ILPTask status is updated,
			// we have to update the SkyCluster object here.
			if optResult == "Optimal" {
				if err := r.Status().Update(ctx, skyCluster); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "failed to update SkyCluster with the result of ILPTask")
				}
			}

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
		logger.Info(fmt.Sprintf("[%s]\t Checking pod status again in 5 sec.", loggerName))
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	// If the status is not running, then it means the optimization was run previously
	// or it is the first time we are running the optimization, so we need to check the
	// status of the optimization (ILPTask.Status.Optimization.Result),
	// If it is empty, then we need to run the optimization
	if instance.Status.Optimization.Result != "" {
		logger.Info(fmt.Sprintf("[%s]\t ILPTask already processed. Skipping the optimization.", loggerName))
		return ctrl.Result{}, nil
	}

	// We need to schedule the optimization,
	// SkyCluster (owner) with same name as the current object,
	// has a list of all components (tasks)
	// that we should include in the optimization process.
	// We iterate over all components in the spec.skyComponents
	// and create corresponding tasks for optimization problem.

	skyCluster := &corev1alpha1.SkyCluster{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, skyCluster); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t SkyCluster not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Creating tasks.csv
	tasks := getTasksFromSkyCluster(skyCluster)
	// Creating task-locations.csv
	// Location constraints should be retrived from deployment policy object
	// with the same name as the current object
	dp := &policyv1alpha1.DeploymentPolicy{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      skyCluster.Spec.DeploymentPolciyRef.Name,
	}, dp); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	taskLocations := getTasksLocationsFromDeployPolicy(dp)
	// Creating tasks-edges.csv
	df := &policyv1alpha1.DataflowPolicy{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      skyCluster.Spec.DataflowPolicyRef.Name,
	}, df); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t DataflowPolicy not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	tasksEdges := getTasksEdgesFromDataflowPolicy(df)

	// Creating task definitions and task attribute files
	// These information are provided by the SkyCluster object
	// The optimization is done by creating a pod, and mounting optimization scripts
	// as well as providers data and components data from the SkyCluster object.
	// The providers files (.json) are generated during the installation phase and stored
	// in a persistent volume.
	// Provider files (generated during the setup)
	//    '/shared/providers.json',
	//    '/shared/providers-attr.json',
	//    '/shared/offerings.json',
	//    '/shared/providers.csv',
	//    '/shared/providers-attr.csv',
	//    '/shared/vservices.csv',
	// Results
	//    '/shared/optimization-stats.csv'
	//    '/shared/deploy-plan.json'
	// The result of the optimization is stored in the deploy-plan.json file and
	// a configmap is created to store the results with label
	// skycluster.io/config-type: optimization-status

	// First we retrive the confimap and then use it within the pod definition
	// optimization-starter contains the scripts to run the optimization
	// optimization-scripts contains the core optimizaiton scripts
	var configMapList map[string]corev1.ConfigMap
	configMapList, err := getOptimizationConfigMaps(ctx, r)
	if err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error listing ConfigMaps (optimization scripts).", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Define Pod
	// The instance name is as the SkyCluster name
	// If they differ the name of SkyCluster should be passed
	pod := defineOptimizationPod(configMapList, instance.Name, tasks, taskLocations, tasksEdges)
	// The pod is in skycluster namespace
	// The ilptask is in the default namespace, and cross namespace reference is not allowed
	// The pod is removed when the ILPTask is not found (deleted).
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
	logger.Info(fmt.Sprintf("[%s]\t ILPTask scheduled. Pod Created. Requeue after 5 sec.", loggerName))
	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

func (r *ILPTaskReconciler) updateSkyCluster(ctx context.Context, req ctrl.Request, deployPlan corev1alpha1.DeployMap, result, status string) (*corev1alpha1.SkyCluster, error) {
	skyCluster := &corev1alpha1.SkyCluster{}
	// It has a same name as the ILPTask
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, skyCluster); err != nil {
		return nil, err
	}
	// The deployment plan is a json string
	skyCluster.Status.Optimization.DeployMap = deployPlan
	skyCluster.Status.Optimization.Status = status
	skyCluster.Status.Optimization.Result = result
	return skyCluster, nil
}

func getTasksFromSkyCluster(skyCluster *corev1alpha1.SkyCluster) string {
	tasks := make([]string, 0)
	tasks = append(tasks, "# header")
	for _, component := range skyCluster.Spec.SkyComponents {
		cmpntName := component.Components.Name
		cmpntKind := component.Components.Kind
		cmpntApiVersion := component.Components.APIVersion
		// Each vs \in component.VirtualServices is a required service for this component
		// and should be appeared in a new line in the tasks.csv
		for _, vs := range component.VirtualServices {
			// we set the quantity to 1 for now,
			// for future sky services we can set the quantity as needed
			vsNames := func(ss []string, t string, c string) string {
				r := ""
				for i, s := range ss {
					r += fmt.Sprintf("%s_%s", t, s)
					if i < len(ss)-1 {
						r += c
					}
				}
				return r
			}(strings.Split(vs.Name, "__"), vs.Type, "__")
			tasks = append(tasks, fmt.Sprintf("%s.%s, %s, %s, %s", cmpntName, strings.ToLower(cmpntKind), cmpntApiVersion, cmpntKind, vsNames))
		}
	}
	return strings.Join(tasks, "\n")
}

func getTasksLocationsFromDeployPolicy(dp *policyv1alpha1.DeploymentPolicy) string {
	taskLocations := make([]string, 0)
	taskLocations = append(taskLocations, "# header")
	for _, dpItem := range dp.Spec.DeploymentPolicies {
		locs_permitted := make([]string, 0)
		locs_required := make([]string, 0)
		for _, loc := range dpItem.LocationConstraint.Permitted {
			locs_permitted = append(locs_permitted, fmt.Sprintf(
				"%s|%s||%s|%s",
				loc.Name,
				loc.Type,
				loc.Region,
				loc.Zone,
			))
		}
		for _, loc := range dpItem.LocationConstraint.Required {
			locs_required = append(locs_required, fmt.Sprintf(
				"%s|%s||%s|%s",
				loc.Name,
				loc.Type,
				loc.Region,
				loc.Zone,
			))
		}
		taskLocations = append(taskLocations, fmt.Sprintf(
			"%s.%s, %s, %s, %s, %s, -1",
			dpItem.ComponentRef.Name,
			strings.ToLower(dpItem.ComponentRef.Kind),
			dpItem.ComponentRef.APIVersion,
			dpItem.ComponentRef.Kind,
			strings.Join(locs_required, "__"), strings.Join(locs_permitted, "__")))
	}
	return strings.Join(taskLocations, "\n")
}

func getTasksEdgesFromDataflowPolicy(df *policyv1alpha1.DataflowPolicy) string {
	taskEdges := make([]string, 0)
	taskEdges = append(taskEdges, "# header")
	for _, df := range df.Spec.DataDependencies {
		srcName := df.From.Name + "." + strings.ToLower(df.From.Kind)
		dstName := df.To.Name + "." + strings.ToLower(df.To.Kind)
		// TODO: currently we set the total data transfer and leave the average data rate,
		// We should later take it into consideration if possible
		taskEdges = append(taskEdges, fmt.Sprintf(
			"%s, %s, %s, %s, -1", srcName, dstName, df.Latency, df.TotalDataTransfer))
	}
	return strings.Join(taskEdges, "\n")
}

func defineOptimizationPod(configMapList map[string]corev1.ConfigMap, skyClusterName, tasks, tasksLocations, tasksEdges string) *corev1.Pod {
	// "optimization-starter", "optimization-scripts"
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
			RestartPolicy:      corev1.RestartPolicyNever,
			InitContainers: []corev1.Container{
				{
					Name: "kubectl",
					// TODO: the image registry should be configurable
					Image: "registry.skycluster.io/kubectl:latest",
					Env: []corev1.EnvVar{
						{Name: "TASKS", Value: tasks},
						{Name: "TASKS_EDGES", Value: tasksEdges},
						{Name: "TASKS_LOCATIONS", Value: tasksLocations},
					},
					Command: []string{
						"/bin/sh",
						"-c",
					},
					Args: []string{
						strings.ReplaceAll(
							configMapList["optimization-starter"].Data["init.sh"],
							"__SKYCLUSTER__NAME__",
							skyClusterName),
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
					Env: []corev1.EnvVar{
						{Name: "TASKS", Value: tasks},
						{Name: "TASKS_EDGES", Value: tasksEdges},
						{Name: "TASKS_LOCATIONS", Value: tasksLocations},
					},
					Args: []string{
						strings.ReplaceAll(
							configMapList["optimization-starter"].Data["main.sh"],
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
								Name: configMapList["optimization-scripts"].Name,
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

func getOptimizationConfigMaps(ctx context.Context, r *ILPTaskReconciler) (map[string]corev1.ConfigMap, error) {
	configMapLabels := []string{"optimization-starter", "optimization-scripts"}
	cfgMapList := make(map[string]corev1.ConfigMap)
	for _, label := range configMapLabels {
		var configMapList corev1.ConfigMapList
		if err := r.List(ctx, &configMapList, client.MatchingLabels{
			corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL:  corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
			corev1alpha1.SKYCLUSTER_CONFIGTYPE_LABEL: label,
		}); err != nil {
			return nil, err
		}
		// we expect to have only one configmap
		if len(configMapList.Items) != 1 {
			return nil, errors.New("multiple configmaps exist (optimization-starter)")
		}
		if len(configMapList.Items) == 0 {
			return nil, errors.New("no configmap found (optimization-starter)")
		}
		cfgMapList[label] = configMapList.Items[0]
	}
	return cfgMapList, nil
}

// Returns:
// - podStatus: The current status of the pod.
// - optimizationStatus: The status of the optimization process.
// - deploymentPlan: The deployment plan details.
// - error: An error object if an error occurred, otherwise nil.
func getPodStatusAndResult(ctx context.Context, r *ILPTaskReconciler) (podStatus string, optmizationStatus string, deployPlan string, err error) {
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
