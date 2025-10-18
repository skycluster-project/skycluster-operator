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
	"os"
	"path/filepath"
	"strconv"
	"strings"

	// "time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller"
)

// +kubebuilder:rbac:groups=core.skycluster.io,resources=latencies,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=ilptasks/finalizers,verbs=update

// ILPTaskReconciler reconciles a ILPTask object
type ILPTaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}


func (r *ILPTaskReconciler) removeOptimizationPod(ctx context.Context, req ctrl.Request) error{
	// Delete the optimization pod best effort
	pod := &corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
		Name:      req.Name,
	}, pod); err != nil {
		return err
	}
	if err := r.Delete(ctx, pod); err != nil {
		return err
	}
	return nil
}

func (r *ILPTaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("ILPTask", req.NamespacedName.Name)
	logger.Info("Reconciling")

	// Fetch the ILPTask instance
	task := &cv1a1.ILPTask{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		logger.Info("ILPTask not found; remove any existing optimization pod")
		_ = r.removeOptimizationPod(ctx, req)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	status := task.Status.Optimization

	// Fetch current resourceVersions of referenced DataflowPolicy and DeploymentPlan.
	dfName := task.Spec.DataflowPolicyRef.Name
	dpName := task.Spec.DeploymentPlanRef.Name

	// If references are empty, treat them as missing (resourceVersion empty).
	df := pv1a1.DataflowPolicy{}
	if dfName != "" {
		if err := r.Get(ctx, req.NamespacedName, &df); err != nil {
			logger.Error(err, "unable to fetch DataflowPolicy", "name", dfName)
			// requeue on error
			return ctrl.Result{}, err
		}
	}
	dp := pv1a1.DeploymentPolicy{}
	if dpName != "" {
		if err := r.Get(ctx, req.NamespacedName, &dp); err != nil {
			logger.Error(err, "unable to fetch DeploymentPlan", "name", dpName)
			return ctrl.Result{}, err
		}
	}

	currDFRV := df.ObjectMeta.ResourceVersion
	currDPRV := dp.ObjectMeta.ResourceVersion

	// If result is already set and referenced resources didn't change -> skip
	if status.Result != "" && status.DataflowResourceVersion == currDFRV && status.DeploymentPlanResourceVersion == currDPRV {
		logger.Info("ILPTask already processed and references unchanged. Skipping optimization.")
		return ctrl.Result{}, nil
	}

	// Otherwise create or ensure optimization pod
	podName := fmt.Sprintf("%s-ilp-opt", task.Name)
	// pod := &corev1.Pod{}
	// err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: podName}, pod)
	// if err == nil {
	// 	// pod exists - check phase
	// 	// podStatus, optResult, optDeployPlan, err := getPodStatusAndResult(ctx, r)
	// 	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
	// 		// Pod completed - read result from a ConfigMap or pod logs in real implementation.
	// 		// For now, we set Result based on PodSucceeded/Failed.

	// 		// instance.Status.Optimization.ConfigMapRef = corev1.LocalObjectReference{
	// 		// 	Name: OPTMIZATION_POD_NAME,
	// 		// }
	// 		// instance.Status.Optimization.PodRef = corev1.LocalObjectReference{
	// 		// 	Name: OPTMIZATION_POD_NAME,
	// 		// }

	// 		if pod.Status.Phase == corev1.PodSucceeded {
	// 			task.Status.Optimization.Status = "Succeeded"
	// 			task.Status.Optimization.Result = "Success"

	// 			// if the result is optimal we set the deployment plan
	// 			// 			deployPlan := hv1a1.DeployMap{}
	// 			// if err = json.Unmarshal([]byte(optDeployPlan), &deployPlan); err != nil {
	// 			// 	logger.Info(fmt.Sprintf("[%s]\t Failed to unmarshal deploy plan", loggerName))
	// 			// 	return ctrl.Result{}, err
	// 			// }
	// 			// instance.Status.Optimization.DeployMap = deployPlan

	// 			// If the optimization result is "Optimal" and status is "Succeeded",
	// 			// We have the deployment plan and we can update the SkyCluster object. (trigger deployment)
	// 			// skyCluster, err = r.updateSkyCluster(ctx, req, deployPlan, "Optimal", "Succeeded")
	// 			// instance.SetCondition("Ready", metav1.ConditionTrue, "ILPTaskCompleted", "ILPTask completed successfully.")
	// 			// if err != nil {
	// 			// 	logger.Info(fmt.Sprintf("[%s]\t Failed to get SkyCluster upon updating with ILPTask results.", loggerName))
	// 			// 	return ctrl.Result{}, err
	// 			// }

	// 		} else {
	// 			task.Status.Optimization.Status = "Failed"
	// 			task.Status.Optimization.Result = "Infeasible"
	// 		}
	// 		// update stored resource versions to current so we won't rerun unnecessarily
	// 		task.Status.Optimization.DataflowResourceVersion = currDFRV
	// 		task.Status.Optimization.DeploymentPlanResourceVersion = currDPRV
	// 		task.Status.Optimization.PodRef = corev1.LocalObjectReference{Name: pod.Name}
	// 		if err := r.Status().Update(ctx, task); err != nil {
	// 			logger.Error(err, "failed to update ILPTask status after pod completion")
	// 			return ctrl.Result{}, err
	// 		}
	// 		logger.Info("Optimization pod completed; status updated", "phase", pod.Status.Phase)
	// 		return ctrl.Result{}, nil
	// 	}
	// 	// Pod still running; ensure status records it (how about failed status?)
	// 	task.Status.Optimization.Status = "Running"
	// 	task.Status.Optimization.PodRef = corev1.LocalObjectReference{Name: pod.Name}
	// 	if err := r.Status().Update(ctx, task); err != nil {
	// 		logger.Error(err, "failed to update status with running pod info")
	// 		return ctrl.Result{}, err
	// 	}
	// 	logger.Info("Optimization pod already running")
	// 	return ctrl.Result{RequeueAfter: 10 * 1e9}, nil // requeue in 10s
	// }

	// Pod not found -> create one to run optimization
	_, err := r.buildOptimizationPod(df, dp, task, podName, req.Namespace)
	if err != nil {
		logger.Error(err, "failed to build optimization pod")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
	// if err := ctrl.SetControllerReference(task, newPod, r.Scheme); err != nil {
	// 	logger.Error(err, "unable to set owner reference on pod")
	// 	return ctrl.Result{}, err
	// }
	// if err := r.Create(ctx, newPod); err != nil {
	// 	logger.Error(err, "failed to create optimization pod")
	// 	return ctrl.Result{}, err
	// }
	// // Update status to reflect new pod and clear previous result so optimization runs fresh
	// task.Status.Optimization.Status = "Pending"
	// task.Status.Optimization.Result = ""
	// task.Status.Optimization.PodRef = corev1.LocalObjectReference{Name: newPod.Name}
	// // do not yet update resource version fields; they'll be updated when pod completes
	// if err := r.Status().Update(ctx, task); err != nil {
	// 	logger.Error(err, "failed to update ILPTask status after creating pod")
	// 	return ctrl.Result{}, err
	// }
	// logger.Info("Optimization pod created", "pod", newPod.Name)
	// return ctrl.Result{RequeueAfter: 5 * 1e9}, nil
}

func (r *ILPTaskReconciler) buildOptimizationPod(df pv1a1.DataflowPolicy, dp pv1a1.DeploymentPolicy, task *cv1a1.ILPTask, podName string, ns string) (*corev1.Pod, error) {
	// must prepare list of all components (tasks)
	// that we should include in the optimization process.
	// We iterate over all components in deployment policy object
	// and create corresponding tasks for optimization problem.

	// Creating tasks.csv
	tasksJson, err := generateTasksJson(dp)
	if err != nil {
		return nil, err
	}
	// Creating tasks-edges.csv
	tasksEdges, err := generateTasksEdgesJson(df)
	if err != nil {
		return nil, err
	}

	providers, err := r.generateProvidersJson()
	if err != nil {
		return nil, err
	}
	providersAttr, err := r.generateProvidersAttrJson(ns)
	if err != nil {
		return nil, err
	}
	virtualServices, err := r.generateVServicesJson()
	if err != nil {
		return nil, err
	}

	// write all JSON strings to a temporary directory for quick inspection
	tmpDir, err := os.MkdirTemp("/tmp", "ilp-debug-")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "tasks.json"), []byte(tasksJson), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tasks-edges.json"), []byte(tasksEdges), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "providers.json"), []byte(providers), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "providers-attr.json"), []byte(providersAttr), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "virtual-services.json"), []byte(virtualServices), 0644); err != nil {
		return nil, err
	}

	
	// Results
	//    '/shared/optimization-stats.csv'
	//    '/shared/deploy-plan.json'
	// The result of the optimization is stored in the deploy-plan.json file and
	// a configmap is created to store the results with label
	// skycluster.io/config-type: optimization-status

	// First we retrive the confimap and then use it within the pod definition
	// optimization-starter contains the scripts to run the optimization
	// optimization-scripts contains the core optimizaiton scripts
	// var configMapList map[string]corev1.ConfigMap
	// configMapList, err := getOptimizationConfigMaps(ctx, r)
	// if err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t Error listing ConfigMaps (optimization scripts).", loggerName))
	// 	return ctrl.Result{}, client.IgnoreNotFound(err)
	// }

	// Define Pod
	// The instance name is as the SkyCluster name
	// If they differ the name of SkyCluster should be passed
	// pod := defineOptimizationPod(configMapList, instance.Name, tasks, taskLocations, tasksEdges)
	
	// The pod is in skycluster namespace
	// The ilptask is in the default namespace, and cross namespace reference is not allowed
	// The pod is removed when the ILPTask is not found (deleted).
	
	// if err := r.Create(ctx, pod); err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t Failed to create Pod", loggerName))
	// 	return ctrl.Result{}, err
	// }

	return nil, nil
}

func (r *ILPTaskReconciler) updateSkyCluster(ctx context.Context, req ctrl.Request, deployPlan hv1a1.DeployMap, result, status string) (*cv1a1.SkyCluster, error) {
	skyCluster := &cv1a1.SkyCluster{}
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

func generateTasksJson(dp pv1a1.DeploymentPolicy) (string, error) {

	type taskStruct struct {
		Name 		 string `json:"name,omitempty"`
		ApiVersion  string `json:"apiVersion,omitempty"`
		Kind       string `json:"kind,omitempty"`
		Count      string `json:"count,omitempty"`
	}

	type locStruct struct {
		Name 	 string `json:"name,omitempty"`
		PType 	string `json:"pType,omitempty"`
		RegionAlias string `json:"regionAlias,omitempty"`
		Region 	string `json:"region,omitempty"`
		Platform 	string `json:"platform,omitempty"`
	}

	type optTaskStruct struct {
		Task               string         `json:"task"`
		ApiVersion         string          `json:"apiVersion"`
		Kind               string          `json:"kind"`
		PermittedLocations []locStruct     `json:"permittedLocations"`
		RequiredLocations  [][]locStruct   `json:"requiredLocations"`
		RequestedVServices [][]taskStruct  `json:"requestedVServices"`
		MaxReplicas        string          `json:"maxReplicas"`
	}

	var optTasks []optTaskStruct
	for _, cmpnt := range dp.Spec.DeploymentPolicies {

		vsList := make([][]taskStruct, 0)
		for _, vs := range cmpnt.VirtualServiceConstraint {
			altVSList := make([]taskStruct, 0)
			for _, alternativeVS := range vs.AnyOf {
				newTask := taskStruct{
					ApiVersion:  alternativeVS.APIVersion,
					Kind:       alternativeVS.Kind,
					Name:      alternativeVS.Name,
					Count:  strconv.Itoa(alternativeVS.Count),
				}
				altVSList = append(altVSList, newTask)
			}
			vsList = append(vsList, altVSList)
		}

		perLocList := make([]locStruct, 0)
		for _, perLoc := range cmpnt.LocationConstraint.Permitted {
			newLoc := locStruct{
				Name:       perLoc.Name,
				PType:      perLoc.Type,
				RegionAlias: perLoc.RegionAlias,
				Region:     perLoc.Region,
				Platform:   perLoc.Platform,
			}
			perLocList = append(perLocList, newLoc)
		}

		reqLocList := make([][]locStruct, 0)
		for _, reqLoc := range cmpnt.LocationConstraint.Required {
			altReqLocList := make([]locStruct, 0)
			for _, altReqLoc := range reqLoc.AnyOf {
				newLoc := locStruct{
					Name:       altReqLoc.Name,
					PType:      altReqLoc.Type,
					RegionAlias: altReqLoc.RegionAlias,
					Region:     altReqLoc.Region,
					Platform:   altReqLoc.Platform,
				}
				altReqLocList = append(altReqLocList, newLoc)
			}
			reqLocList = append(reqLocList, altReqLocList)
		}

		optTasks = append(optTasks, optTaskStruct{
			Task:               cmpnt.ComponentRef.Name,
			ApiVersion:         cmpnt.ComponentRef.APIVersion,
			Kind:               cmpnt.ComponentRef.Kind,
			PermittedLocations: perLocList,
			RequiredLocations:  reqLocList,
			RequestedVServices: vsList,
			MaxReplicas:        "-1",
		})
	}
	optTasksJson, err := json.Marshal(optTasks)
	if err != nil {
		return "", fmt.Errorf("failed to marshal optimization tasks: %v", err)
	}
	return string(optTasksJson), nil
}

func generateTasksEdgesJson(df pv1a1.DataflowPolicy) (string, error) {
	type taskEdgeStruct struct {
		SrcTask          string `json:"srcTask,omitempty"`
		DstTask          string `json:"dstTask,omitempty"`
		Latency         string `json:"latency,omitempty"`
		DataRate       string `json:"dataRate,omitempty"`
	}
	var taskEdges []taskEdgeStruct
	for _, df := range df.Spec.DataDependencies {
		taskEdges = append(taskEdges, taskEdgeStruct{
			SrcTask:  df.From.Name,
			DstTask:  df.To.Name,
			Latency:  df.Latency,
			DataRate: df.TotalDataTransfer,
		})
	}
	b, err := json.Marshal(taskEdges)
	if err != nil {
		return "", fmt.Errorf("failed to marshal optimization task edges: %v", err)
	}
	return string(b), nil
}

func (r *ILPTaskReconciler) generateProvidersJson() (string, error) {
	// fetch all providerprofiles objects
	// and generate the providers.json file
	var providers cv1a1.ProviderProfileList
	if err := r.List(context.Background(), &providers); err != nil {
		return "", err
	}

	type providerStruct struct {
		Name        string `json:"name,omitempty"`
		Platform   string `json:"platform,omitempty"`
		RegionAlias string `json:"regionAlias,omitempty"`
		Zone       string `json:"zone,omitempty"`
		PType      string `json:"pType,omitempty"`
		Region     string `json:"region,omitempty"`
	}
	var providerList []providerStruct
	for _, p := range providers.Items {
		for _, zone := range p.Spec.Zones {
			if !zone.Enabled {
				continue
			}
			providerList = append(providerList, providerStruct{
				Name:        p.Name,
				Platform:    p.Spec.Platform,
				PType:      zone.Type,
				RegionAlias: p.Spec.RegionAlias,
				Region:     p.Spec.Region,
				Zone:       zone.Name,
			})
		}
	}
	b, err := json.Marshal(providerList)
	if err != nil {
		return "", fmt.Errorf("failed to marshal providers: %v", err)
	}
	return string(b), nil
}

func (r *ILPTaskReconciler) generateProvidersAttrJson(ns string) (string, error) {
	// fetch all providerprofiles objects
	// and generate the providers.json file
	var providers cv1a1.ProviderProfileList
	if err := r.List(context.Background(), &providers); err != nil {
		return "", err
	}

	type providerAttrStruct struct {
		SrcName 	string  `json:"srcName,omitempty"`
		DstName 	string  `json:"dstName,omitempty"`
		Latency 	float64 `json:"latency"`
		EgressCostDataRate float64 `json:"egressCost_dataRate"`
	}

	var providerList []providerAttrStruct
	for _, p := range providers.Items {
		for _, p2 := range providers.Items {
			if p.Name == p2.Name {
				continue
			}
			a, b := utils.CanonicalPair(p.Name, p2.Name)
			latName := "latency-" + utils.SanitizeName(a) + "-" + utils.SanitizeName(b)
			
			// assuming we are within the first tier of egress cost specs
			// and it is for internet
			egressCost := p.Status.EgressCostSpecs[0].Tiers[0].PricePerGB
			
			var latencyValue string
			lt := &cv1a1.Latency{}
			err := r.Get(context.TODO(), client.ObjectKey{
				Namespace: ns,
				Name:      latName,
			}, lt)
			if err != nil {
				return "", errors.Wrapf(err, "failed to get latency object %s, ns: %s", latName, ns)
			} else {
				if lt.Status.P95 != "" {
					latencyValue = lt.Status.P95
				} else if lt.Status.P99 != "" {
					latencyValue = lt.Status.P99
				} else if lt.Status.LastMeasuredMs != "" {
					latencyValue = lt.Status.LastMeasuredMs
				} else {
					return "", fmt.Errorf("no latency value found in latency object %s", latName)
				}
			}
			latencyValueFloat, err1 := strconv.ParseFloat(latencyValue, 64)
			egressCostFloat, err2 := strconv.ParseFloat(egressCost, 64)
			if err1 != nil || err2 != nil {
				return "", fmt.Errorf("failed to parse latency or egress cost to float for providers %s and %s", p.Name, p2.Name)
			}

			providerList = append(providerList, providerAttrStruct{
				SrcName: 	p.Name,
				DstName: 	p2.Name,
				Latency: 	latencyValueFloat,
				EgressCostDataRate: egressCostFloat,
			})
		}
	}
	b, err := json.Marshal(providerList)
	if err != nil {
		return "", fmt.Errorf("failed to marshal providers: %v", err)
	}
	return string(b), nil
}

func (r *ILPTaskReconciler) generateVServicesJson() (string, error) {
	// Virtual Services are defined manually for now
	// Available virtual services:
	// ComputeProfile "flavors.yaml"
	// ManagedKubernetes "managed-k8s.yaml"

	// List all config maps by labels
	var configMaps corev1.ConfigMapList
	if err := r.List(context.Background(), &configMaps, client.MatchingLabels{
		"skycluster.io/config-type": "provider-profile",
		"skycluster.io/managed-by": "skycluster",
	}); err != nil {
		return "", err
	}


	type vServiceStruct struct {
		VServiceName 	string  `json:"vservice_name,omitempty"`
		VServiceKind 	string  `json:"vservice_kind,omitempty"`
		ProviderName 	string  `json:"provider_name,omitempty"`
		ProviderPlatform string  `json:"provider_platform,omitempty"`
		ProviderRegion 	string  `json:"provider_region,omitempty"`
		ProviderZone 	string  `json:"provider_zone,omitempty"`
		DeployCost  	float64 `json:"deploy_cost,omitempty"`
		Availability 	int     `json:"availability,omitempty"`
	}
	var vServicesList []vServiceStruct

	for _, cm := range configMaps.Items {
		pName := cm.Labels["skycluster.io/provider-profile"]
		pPlatform := cm.Labels["skycluster.io/provider-platform"]
		pRegion := cm.Labels["skycluster.io/provider-region"]
		
		cmData, ok := cm.Data["flavors.yaml"]
		if ok {
			var zoneOfferings []cv1a1.ZoneOfferings
			if err := yaml.Unmarshal([]byte(cmData), &zoneOfferings); err == nil {
				for _, zo := range zoneOfferings {
					for _, of := range zo.Offerings{
						priceFloat, err := strconv.ParseFloat(of.Price, 64)
						if err != nil {continue}
						vServicesList = append(vServicesList, vServiceStruct{
							VServiceName:   of.NameLabel,
							VServiceKind:   "ComputeProfile",
							ProviderName:   pName,
							ProviderPlatform: pPlatform,
							ProviderRegion: pRegion,
							ProviderZone:   zo.Zone,
							DeployCost:     priceFloat,
							Availability:   10000, // assuming always available
						})
					}
				}
			}
		}

		cmData, ok = cm.Data["managed-k8s.yaml"]
		if !ok { continue }
		var managedK8s []hv1a1.ManagedK8s
		if err := yaml.Unmarshal([]byte(cmData), &managedK8s); err != nil {
			return "", fmt.Errorf("failed to unmarshal managed k8s config map: %v", err)
		}
		for _, mk8s := range managedK8s {
			// I expect only one offering
			priceFloat, err1 := strconv.ParseFloat(mk8s.Price, 64)
			priceOverheadFloat, err2 := strconv.ParseFloat(mk8s.Overhead.Cost, 64)
			if err1 != nil || err2 != nil {
				return "", fmt.Errorf("failed to parse price or overhead for managed k8s vservice %s: price error: %v; overhead error: %v", mk8s.Name, err1, err2)
			}
			vServicesList = append(vServicesList, vServiceStruct{
				VServiceName:   mk8s.Name,
				VServiceKind:   "ManagedKubernetes",
				ProviderName:   pName,
				ProviderPlatform: pPlatform,
				ProviderRegion: pRegion,
				DeployCost:     priceFloat + priceOverheadFloat,
				Availability:   10000, // assuming always available
			})
		}
	}
	b, err := json.Marshal(vServicesList)
	if err != nil {
		return "", fmt.Errorf("failed to marshal virtual services: %v", err)
	}
	return string(b), nil
}

func defineOptimizationPod(configMapList map[string]corev1.ConfigMap, skyClusterName, tasks, tasksLocations, tasksEdges string) *corev1.Pod {
	// "optimization-starter", "optimization-scripts"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "OPTMIZATION_POD_NAME",
			Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
			Labels: map[string]string{
				hv1a1.SKYCLUSTER_MANAGEDBY_LABEL: hv1a1.SKYCLUSTER_MANAGEDBY_VALUE,
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
							"OPTMIZATION_POD_NAME"),
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
			hv1a1.SKYCLUSTER_MANAGEDBY_LABEL:  hv1a1.SKYCLUSTER_MANAGEDBY_VALUE,
			hv1a1.SKYCLUSTER_CONFIGTYPE_LABEL: label,
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
		Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
		Name:      "OPTMIZATION_POD_NAME",
	}, pod); err != nil {
		return "", "", "", err
	}
	if pod.Status.Phase == corev1.PodSucceeded {
		// get the result from the configmap
		configMap := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
			Name:      "OPTMIZATION_POD_NAME",
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
		For(&cv1a1.ILPTask{}).
		Named("core-ilptask").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}
