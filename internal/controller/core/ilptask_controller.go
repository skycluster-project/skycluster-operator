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
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	// "time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

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
		if err := r.ensureSkyXRD(task, appId1, status.DeployMap); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to ensure SkyXRD after optimization")
		}
		if err := r.ensureSkyNet(task, appId1, status.DeployMap); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to ensure SkyNet after optimization")
		}
		return ctrl.Result{}, nil
	}

	// Otherwise create or ensure optimization pod
	podName := task.Status.Optimization.PodRef.Name

	if podName == "" { // not yet created
		// Pod not found -> create one to run optimization
		r.Logger.Info("Creating optimization pod for ILPTask")
		_, err := r.prepareAndBuildOptimizationPod(df, dp, task)
		if err != nil { return ctrl.Result{}, err }
		
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
					deployPlan := hv1a1.DeployMap{}
					if err = json.Unmarshal([]byte(optDeployPlan), &deployPlan); err != nil {
						return ctrl.Result{}, errors.Wrap(err, "failed to unmarshal deploy plan")
					}
					task.Status.Optimization.DeployMap = deployPlan
					if len(deployPlan.Component) > 0 && len(deployPlan.Edges) > 0 {
						// If the optimization result is "Optimal" and status is "Succeeded",
						// We have the deployment plan and we can update the SkyCluster object. (trigger deployment)
						// skyCluster, err = r.updateSkyCluster(ctx, req, deployPlan, "Optimal", "Succeeded")
						task.Status.Optimization.Status = "Succeeded"
						task.Status.Optimization.Result = "Success"
						task.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "OptimizationSucceeded", "ILPTask optimization succeeded")

						// generate skyXRD object
						if err = r.ensureSkyXRD(task, appId1, deployPlan); err != nil {
							return ctrl.Result{}, errors.Wrap(err, "failed to ensure SkyXRD after optimization")
						}
						if err = r.ensureSkyNet(task, appId1, deployPlan); err != nil {
							return ctrl.Result{}, errors.Wrap(err, "failed to ensure SkyNet after optimization")
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
			r.Logger.Error(err, "failed to get optimization pod", "podName", podName)
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
		}
	}
	return ctrl.Result{}, nil
}

func (r *ILPTaskReconciler) prepareAndBuildOptimizationPod(df pv1a1.DataflowPolicy, dp pv1a1.DeploymentPolicy, task *cv1a1.ILPTask) (string, error) {
	// Creating tasks.csv
	
	// write all JSON strings to a temporary directory for quick inspection
	tmpDir, err := os.MkdirTemp("/tmp", "ilp-debug-")
	if err != nil {
		return "", err
	}

	tasksJson, err := r.generateTasksJson(dp)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tasks.json"), []byte(tasksJson), 0644); err != nil {
		return "", err
	}

	// Creating tasks-edges.csv
	tasksEdges, err := generateTasksEdgesJson(df)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tasks-edges.json"), []byte(tasksEdges), 0644); err != nil {
		return "", err
	}

	providers, err := r.generateProvidersJson()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "providers.json"), []byte(providers), 0644); err != nil {
		return "", err
	}

	providersAttr, err := r.generateProvidersAttrJson(hv1a1.SKYCLUSTER_NAMESPACE)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "providers-attr.json"), []byte(providersAttr), 0644); err != nil {
		return "", err
	}

	// virtualServices, err := r.generateVServicesJson()
	// if err != nil {
	// 	return "", err
	// }
	// if err := os.WriteFile(filepath.Join(tmpDir, "virtual-services.json"), []byte(virtualServices), 0644); err != nil {
	// 	return "", err
	// }

	dataMap := map[string]string{
		"tasks.json":         tasksJson,
		"tasks-edges.json":   tasksEdges,
		"providers.json":     providers,
		"providers-attr.json": providersAttr,
		// "virtual-services.json": virtualServices,
	}

	// Results
	//    '/shared/optimization-stats.csv'
	//    '/shared/deploy-plan.json'
	// The result of the optimization is stored in the deploy-plan.json file and
	// a configmap is created to store the results with label
	// skycluster.io/config-type: optimization-status

	scripts, err := r.getOptimizationConfigMaps()
	if err != nil { return "", err }

	// Define Pod
	// If they differ the name of SkyCluster should be passed
	podName, err := r.buildOptimizationPod(task, scripts, dataMap)
	if err != nil {
		return "", err
	}

	return podName, nil
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

func (r *ILPTaskReconciler) generateTasksJson(dp pv1a1.DeploymentPolicy) (string, error) {
	type virtualSvcStruct struct {
		Name 		 string `json:"name,omitempty"`
		ApiVersion  string `json:"apiVersion,omitempty"`
		Kind       string `json:"kind,omitempty"`
		Count      string `json:"count,omitempty"`
		Price      float64 `json:"price,omitempty"`
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
		RequestedVServices [][]virtualSvcStruct  `json:"requestedVServices"`
		MaxReplicas        string          `json:"maxReplicas"`
	}

	var optTasks []optTaskStruct
	if len(dp.Spec.DeploymentPolicies) == 0 {
		return "", fmt.Errorf("no deployment policies found in deployment policy")
	}
	for _, cmpnt := range dp.Spec.DeploymentPolicies {
		if len(cmpnt.VirtualServiceConstraint) == 0 {
			return "", fmt.Errorf("no virtual service constraints found for component %s", cmpnt.ComponentRef.Name)
		}
		vsList := make([][]virtualSvcStruct, 0)
		for _, vsc := range cmpnt.VirtualServiceConstraint {
			if len(vsc.AnyOf) == 0 {
				return "", fmt.Errorf("no virtual service alternatives found for component %s, %v", cmpnt.ComponentRef.Name, cmpnt.VirtualServiceConstraint)
			}
			altVSList := make([]virtualSvcStruct, 0)
			additionalVSList := make([]virtualSvcStruct, 0)
			for _, alternativeVS := range vsc.AnyOf {
				newVS := virtualSvcStruct{
					ApiVersion:  alternativeVS.APIVersion,
					Kind:       alternativeVS.Kind,
					Name:      alternativeVS.Name,
					Count:  strconv.Itoa(alternativeVS.Count),
				}
				altVSList = append(altVSList, newVS)
				
				// If this alternative is a managed Kubernetes offering, 
				// add all potential ComputeProfile alternatives,
				// Optimizer will choose the best one based on cost and requirements.
				// This can be disabled later if needed
				if strings.EqualFold(alternativeVS.Name, "ManagedKubernetes") {
					r.Logger.Info("Adding ComputeProfile alternatives for ManagedKubernetes virtual service", "component", cmpnt.ComponentRef.Name, "managedK8sName", alternativeVS.Name)
					// Compute the minimum compute resource required for this component/deployment
					minCR, err := r.calculateMinComputeResource(dp.Namespace, cmpnt.ComponentRef.Name)
					if err != nil {
						return "", fmt.Errorf("failed to calculate min compute resource for component %s: %w", cmpnt.ComponentRef.Name, err)
					}
					if minCR == nil {
						return "", fmt.Errorf("nil min compute resource for component %s", cmpnt.ComponentRef.Name)
					}

					// r.Logger.Info("Minimum compute resource for component", "component", cmpnt.ComponentRef.Name, "cpu", minCR.cpu, "ram", minCR.ram)

					// Try to fetch flavors for the provider identified by alternativeVS.Name (fallback to component name)
					profiles, err := r.getAllComputeProfiles(cmpnt.LocationConstraint.Permitted)
					if err != nil { return "", err }

					// r.Logger.Info("Found compute profiles for component", "component", cmpnt.ComponentRef.Name, "numProfiles", len(profiles))
					
					// // find the cheapest offering
					// var cheapest *computeProfileService
					// for _, off := range profiles {
					// 	if off.cpu < minCR.cpu { continue }
					// 	if off.ram < minCR.ram { continue } // GiB
						
					// 	if cheapest == nil || off.deployCost < cheapest.deployCost {
					// 		tmp := off
					// 		cheapest = &tmp
					// 	}
					// }
					// if cheapest == nil {
					// 	r.Logger.Info("No suitable compute profile found for component, may not be deployable, Optimization is not accurate.", "component", cmpnt.ComponentRef.Name)
					// }
					// // Add the cheapest offering as a ComputeProfile alternative
					// cpVS := virtualSvcStruct{
					// 	ApiVersion: alternativeVS.APIVersion, 
					// 	Kind:       "ComputeProfile",
					// 	Name:       cheapest.name, // or NameLabel if you prefer
					// 	Count:      "1",
					// }
					// altVSList = append(altVSList, cpVS)

					// collect all suitable offerings
					for _, off := range profiles {
						if off.cpu < minCR.cpu { continue }
						if off.ram < minCR.ram { continue } // GiB

						// Add this offering as a ComputeProfile alternative
						additionalVSList = append(additionalVSList, virtualSvcStruct{
								ApiVersion: alternativeVS.APIVersion,
								Kind:       "ComputeProfile",
								Name:       off.name, // name is mapped to nameLabel
								Count:      "1",
								Price:      off.deployCost,
						})
					}
					// the list can be huge, so sort and limit to top 5 cheapest
					sort.Slice(additionalVSList, func(i, j int) bool {
						return additionalVSList[i].Price < additionalVSList[j].Price
					})
					// remove duplicates
					additionalVSList = lo.UniqBy(additionalVSList, func(v virtualSvcStruct) string {
						return v.Name
					})
					if len(additionalVSList) > 5 {
						additionalVSList = additionalVSList[:5]
					}
				}
			}
			if len(altVSList) == 0 {
				return "", fmt.Errorf("no virtual service alternatives found for component %s, %v", cmpnt.ComponentRef.Name, cmpnt.VirtualServiceConstraint)
			}
			vsList = append(vsList, altVSList)
			vsList = append(vsList, additionalVSList)
		}

		perLocList := make([]locStruct, 0)
		if len(cmpnt.LocationConstraint.Permitted) == 0 {
			return "", fmt.Errorf("no permitted locations found for component %s", cmpnt.ComponentRef.Name)
		}
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
	if len(optTasks) == 0 {
		return "", fmt.Errorf("no optimization tasks found in deployment policy")
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
				// we must have zero latency to self
				providerList = append(providerList, providerAttrStruct{
					SrcName: 	p.Name,
					DstName: 	p2.Name,
					Latency: 	0.0,
					EgressCostDataRate: 0.0,
				})
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

func (r *ILPTaskReconciler) getAllComputeProfiles(provRefs []hv1a1.ProviderRefSpec) ([]computeProfileService, error) {
	provProfiles := make([]cv1a1.ProviderProfileSpec, 0)
	for _, provRef := range provRefs {
		provProfiles = append(provProfiles, cv1a1.ProviderProfileSpec{
			Platform: provRef.Platform,
			Region:   provRef.Region,
			RegionAlias: provRef.RegionAlias,
		})
	}

	candidates, err := r.getCandidateProviders(provProfiles)
	if err != nil { return nil, err }

	allComputeProfiles := make([]computeProfileService, 0)
	for _, c := range candidates {
		profiles, err := r.getComputeProfileForProvider(c)
		if err != nil {
			return nil, err
		}
		allComputeProfiles = append(allComputeProfiles, profiles...)
	}

	return allComputeProfiles, nil
}

func (r *ILPTaskReconciler) getCandidateProviders(filter []cv1a1.ProviderProfileSpec) ([]cv1a1.ProviderProfile, error) {
	providers := cv1a1.ProviderProfileList{}
	err := r.List(context.Background(), &providers)
	if err != nil {
		return nil, err
	}
	candidateProviders := make([]cv1a1.ProviderProfile, 0)
	for _, item := range providers.Items {
		for _, ref := range filter {
			match := true
			if ref.Platform != "" && item.Spec.Platform != ref.Platform { match = false }
			if ref.Region != "" && item.Spec.Region != ref.Region { match = false }
			if ref.RegionAlias != "" && item.Spec.RegionAlias != ref.RegionAlias { match = false }
			if match { candidateProviders = append(candidateProviders, item) }
		}
	}

	return candidateProviders, nil
}

func (r *ILPTaskReconciler) getComputeProfileForProvider(p cv1a1.ProviderProfile) ([]computeProfileService, error) {
	
	// List all config maps by labels
	var configMaps corev1.ConfigMapList
	if err := r.List(context.Background(), &configMaps, client.MatchingLabels{
		"skycluster.io/config-type": "provider-profile",
		"skycluster.io/managed-by": "skycluster",
		"skycluster.io/provider-profile": p.Name,
		"skycluster.io/provider-platform": p.Spec.Platform,
		"skycluster.io/provider-region": p.Spec.Region,
	}); err != nil {
		return nil, err
	}

	if len(configMaps.Items) == 0 {
		return nil, fmt.Errorf("no config maps found for provider %s", p.Name)
	}
	if len(configMaps.Items) > 1 {
		return nil, fmt.Errorf("multiple config maps found for provider %s", p.Name)
	}

	var vServicesList []computeProfileService
	cmData, ok := configMaps.Items[0].Data["flavors.yaml"]
	if ok {
		var zoneOfferings []cv1a1.ZoneOfferings
		if err := yaml.Unmarshal([]byte(cmData), &zoneOfferings); err == nil {
			for _, zo := range zoneOfferings {
				for _, of := range zo.Offerings{
					priceFloat, err := strconv.ParseFloat(of.Price, 64)
					if err != nil {continue}
					fRam, err1 := parseMemory(of.RAM)
					fCPU := float64(of.VCPUs)
					if err1 != nil {continue }

					vServicesList = append(vServicesList, computeProfileService{
						name:   of.NameLabel,
						cpu: 	fCPU,
						ram: fRam,
						gpuEnabled:    of.GPU.Enabled,
						gpuManufacturer: of.GPU.Manufacturer,
						deployCost:     priceFloat,
					})
				}
			}
		}
	}
	
	return vServicesList, nil
}

func parseMemory(s string) (float64, error) {
	var numRe = regexp.MustCompile(`([+-]?\d*\.?\d+(?:[eE][+-]?\d+)?)`)
	m := numRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, fmt.Errorf("no number found in %q", s)
	}
	return strconv.ParseFloat(m[1], 64)
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
				VServiceName:   mk8s.NameLabel,
				VServiceKind:   "ManagedKubernetes",
				ProviderName:   pName,
				ProviderPlatform: pPlatform,
				ProviderRegion: pRegion,
				DeployCost:     priceFloat + priceOverheadFloat,
				Availability:   100000, // assuming always available
			})
		}
	}
	b, err := json.Marshal(vServicesList)
	if err != nil {
		return "", fmt.Errorf("failed to marshal virtual services: %v", err)
	}
	return string(b), nil
}

// dataMap contains the application and provider data (i.e., tasks.json, providers.json, etc.)
func (r *ILPTaskReconciler) buildOptimizationPod(taskMeta *cv1a1.ILPTask, scripts map[string]string, dataMap map[string]string) (string, error) {

	if len(scripts) == 0 || len(dataMap) == 0 {
		return "", fmt.Errorf("optimization scripts or data configmap is empty")
	}

	scriptFiles := make([]string, 0)
	for file, content := range scripts {
		// Use a heredoc for safe multiline content
    heredoc := fmt.Sprintf("cat << 'EOF' > /scripts/%s\n%s\nEOF", file, content)
    scriptFiles = append(scriptFiles, heredoc)
	}
	initCommandForScripts := strings.Join(scriptFiles, "\n")
	
	// Split into separate init containers since some files may be large
	filesMap := make(map[string]string)
	for file, content := range dataMap {
		// Use a heredoc for safe multiline content
    heredoc := fmt.Sprintf("cat << 'EOF' > /shared/%s\n%s\nEOF", file, content)
    filesMap[file] = heredoc
	}
	
	initContainers := make([]corev1.Container, 0)
	initContainers = append(initContainers, corev1.Container{
		Name:  "prepare-vservices",
		Image: "etesami/optimizer-helper:latest",
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{"/vservices"},
		Env: []corev1.EnvVar{
			{
				Name:  "OUTPUT_PATH",
				Value: "/shared/virtual-services.json",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "shared",
				MountPath: "/shared",
			},
		},
	})
	for file, content := range filesMap {
		initContainers = append(initContainers, corev1.Container{
			Name:  "prepare-" + strings.ReplaceAll(file, ".", "-"),
			Image: "busybox",
			Command: []string{"/bin/sh", "-c"},
			Args:    []string{content},
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
		})
	}
	initContainers = append(initContainers, corev1.Container{
		Name:  "prepare-scripts",
		Image: "busybox",
		Command: []string{"/bin/sh","-c"},
		Args:    []string{initCommandForScripts},
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
	},)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: taskMeta.Name + "-",
			Namespace:    hv1a1.SKYCLUSTER_NAMESPACE,
			Labels: map[string]string{
				"skycluster.io/managed-by": "skycluster",
				"skycluster.io/component":  "optimization",
				"ilptask": taskMeta.Name, 
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "skycluster-sva",
			RestartPolicy:      corev1.RestartPolicyNever,
			InitContainers: 	 initContainers,
			Containers: []corev1.Container{
				{
					Name:  "ubuntu-python",
					Image: "etesami/optimizer-engine:latest",
					ImagePullPolicy: corev1.PullAlways,
					Command: []string{
						"/bin/bash", "-c",
					},
					Args: []string{"chmod +x /scripts/start.sh && /scripts/start.sh"},
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
						EmptyDir: &corev1.EmptyDirVolumeSource{},
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
	// The pod is in skycluster namespace
	// The ilptask is in the default namespace, and cross namespace reference is not allowed
	
	// 1) Check for any existing optimization pod for this ILPTask (someone else may have created it)
	podFound := false
	var podList corev1.PodList
	if err := r.List(context.TODO(), &podList,
		client.InNamespace(hv1a1.SKYCLUSTER_NAMESPACE),
		client.MatchingLabels{"skycluster.io/component": "optimization", "ilptask": taskMeta.Name},
	); err == nil {
		for _, p := range podList.Items {
			// prefer a pod that is not terminating
			if p.DeletionTimestamp == nil {
				r.Logger.Info("Found existing optimization pod for ILPTask; reusing", "pod", p.Name)
				podFound = true
				pod = &p
				break
			}
		}
	}
	
	if !podFound {
		if err := r.Create(context.TODO(), pod); err != nil { return "", err }
		r.Logger.Info("Created new optimization pod for ILPTask", "pod", pod.Name)
	}

	// 3) Attempt to "claim" the pod by patching the ILPTask status (so only one reconcile will succeed)
	// Fetch the ILPTask as it is on the server
	orig := &cv1a1.ILPTask{}
	if err := r.Get(context.TODO(), client.ObjectKey{
		Namespace: taskMeta.Namespace,
		Name:      taskMeta.Name,
	}, orig); err != nil { // Cannot fetch ILPTask to claim; best-effort cleanup created pod
		_ = r.Delete(context.TODO(), pod)
		return "", errors.Wrapf(err, "failed to get ILPTask %s/%s to claim pod; deleted created pod", taskMeta.Namespace, taskMeta.Name)
	}


	if orig.Status.Optimization.PodRef.Name != "" {
			// Another reconcile already claimed a pod; delete what we created and return the claimed pod
			r.Logger.Info("ILPTask already has a claimed optimization pod; cleaning up created pod", "claimedPod", orig.Status.Optimization.PodRef.Name, "createdPod", pod.Name)
			_ = r.Delete(context.TODO(), pod)
			return orig.Status.Optimization.PodRef.Name, nil
	}

	// Patch the ILPTask status to set the pod ref using MergeFrom(orig) to detect conflicts
	updated := orig.DeepCopy()
	updated.Status.Optimization.PodRef = corev1.LocalObjectReference{Name: pod.Name}

	if err := r.Status().Patch(context.TODO(), updated, client.MergeFrom(orig)); err != nil {
		// Conflict or other error while claiming; delete created pod and return error
		_ = r.Delete(context.TODO(), pod)
		return "", errors.Wrap(err, "conflict while claiming pod and update the status. Deleted created pod")
	}

	r.Logger.Info("Successfully claimed optimization pod for ILPTask", "pod", pod.Name)
	return pod.Name, nil
}

func (r *ILPTaskReconciler) getOptimizationConfigMaps() (map[string]string, error) {
	scripts := make(map[string]string)
	var configMapList corev1.ConfigMapList
	if err := r.List(context.TODO(), &configMapList, client.MatchingLabels{
		"skycluster.io/managed-by":  "skycluster",
		hv1a1.SKYCLUSTER_CONFIGTYPE_LABEL: "optimization-scripts",
	}); err != nil { return nil, err }

	if len(configMapList.Items) == 0 {
		return nil, errors.New("no configmap found (optimization-starter)")
	}

	for _, cm := range configMapList.Items {
		for key, val := range cm.Data {
			scripts[key] = val
		}
	}
	
	return scripts, nil
}

// Returns:
// - podStatus: The current status of the pod.
// - optimizationStatus: The status of the optimization process.
// - deploymentPlan: The deployment plan details.
// - error: An error object if an error occurred, otherwise nil.
func (r *ILPTaskReconciler) getPodStatusAndResult(podName string) (deployPlan string, err error) {
	pod := &corev1.Pod{}
	if err := r.Get(context.TODO(), client.ObjectKey{
		Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
		Name:      podName,
	}, pod); err != nil {
		return "", err
	}
	if pod.Status.Phase == corev1.PodSucceeded {
		// get the result from the configmap
		configMap := &corev1.ConfigMap{}
		if err := r.Get(context.TODO(), client.ObjectKey{
			Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
			Name:      "deploy-plan-config",
		}, configMap); err != nil {
			return "", err
		}
		// The result of the optimization could be Optimal or Infeasible
		return configMap.Data["deploy-plan.json"], nil
	}
	// When the pod is not completed yet or not succeeded
	// there is no result to return except the pod status
	return "", nil
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


func (r *ILPTaskReconciler) ensureSkyNet(task *cv1a1.ILPTask, appId string, deployPlan hv1a1.DeployMap) error {
	obj := &cv1a1.SkyNetList{}
	if err := r.List(context.TODO(), obj, client.InNamespace(task.Namespace), client.MatchingLabels{
		"skycluster.io/app-id": appId,
	}); err != nil {
		return errors.Wrapf(err, "failed to list SkyNets for ILPTask %s", task.Name)
	}
	if len(obj.Items) > 1 {
		return fmt.Errorf("multiple SkyNets found for ILPTask %s and appId %s", task.Name, appId)
	}
	if len(obj.Items) > 0 {
		// already exists, update if necessary
		obj := &obj.Items[0]
		if !reflect.DeepEqual(obj.Spec.DeployMap, deployPlan) {
			r.Logger.Info("Updating existing SkyNet with new deployment plan", "SkyNet", obj.Name)
			obj.Spec.DeployMap = deployPlan
			obj.Spec.Approve = false
			if err := r.Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update SkyNet for ILPTask %s", task.Name)
			}
			obj.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "PendingApproval", "SkyNet pending approval")
			if err := r.Status().Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update SkyNet status for ILPTask %s", task.Name)
			}
		}
		return nil
	} else { // try to create
		r.Logger.Info("Creating new SkyNet for deployment plan", "ILPTask", task.Name)
		obj := &cv1a1.SkyNet{
			ObjectMeta: metav1.ObjectMeta{
				Name: task.Name + "-" + RandSuffix(task.Name),
				Labels: map[string]string{"skycluster.io/app-id": appId},
				Namespace:    task.Namespace,
			},
			Spec: cv1a1.SkyNetSpec{
				Approve: false,
				DataflowPolicyRef:  task.Spec.DataflowPolicyRef,
				DeploymentPolicyRef:  task.Spec.DeploymentPolicyRef,
				DeployMap: deployPlan,
			},
		}
		if err := r.Create(context.TODO(), obj); err != nil {
			return errors.Wrapf(err, "failed to create SkyXRD for ILPTask %s", task.Name)
		}
	}

	return nil
}

func (r *ILPTaskReconciler) ensureSkyXRD(task *cv1a1.ILPTask, appId string, deployPlan hv1a1.DeployMap) error {
	obj := &cv1a1.SkyXRDList{}
	if err := r.List(context.TODO(), obj, client.InNamespace(task.Namespace), client.MatchingLabels{
		"skycluster.io/app-id": appId,
	}); err != nil {
		return errors.Wrapf(err, "failed to list SkyXRDs for ILPTask %s", task.Name)
	}
	if len(obj.Items) > 1 {
		return fmt.Errorf("multiple SkyXRDs found for ILPTask %s and appId %s", task.Name, appId)
	}
	if len(obj.Items) > 0 { // already exists, update if necessary
		obj := &obj.Items[0]
		if !reflect.DeepEqual(obj.Spec.DeployMap, deployPlan) {
			r.Logger.Info("Updating existing SkyXRD with new deployment plan", "SkyXRD", obj.Name)
			obj.Spec.DeployMap = deployPlan
			obj.Spec.Approve = false
			if err := r.Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update SkyXRD for ILPTask %s", task.Name)
			}
			obj.Status.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "PendingApproval", "SkyXRD pending approval")
			if err := r.Status().Update(context.TODO(), obj); err != nil {
				return errors.Wrapf(err, "failed to update SkyXRD status for ILPTask %s", task.Name)
			}
		}
		return nil
	} else { // try to create
		r.Logger.Info("Creating new SkyXRD for deployment plan", "ILPTask", task.Name)
		obj := &cv1a1.SkyXRD{
			ObjectMeta: metav1.ObjectMeta{
				Name: task.Name + "-" + RandSuffix(task.Name),
				Labels: map[string]string{"skycluster.io/app-id": appId},
				Namespace:    task.Namespace,
			},
			Spec: cv1a1.SkyXRDSpec{
				Approve: false,
				DataflowPolicyRef:  task.Spec.DataflowPolicyRef,
				DeploymentPolicyRef:  task.Spec.DeploymentPolicyRef,
				DeployMap: deployPlan,
			},
		}
		if err := r.Create(context.TODO(), obj); err != nil {
			return errors.Wrapf(err, "failed to create SkyXRD for ILPTask %s", task.Name)
		}
	}

	return nil
}


func (r *ILPTaskReconciler) removeOptimizationPod(ctx context.Context, taskName string) error {
	// Delete the optimization pod best effort
	pod := &corev1.PodList{}
	if err := r.List(ctx, pod, client.MatchingLabels{
		"skycluster.io/component": "optimization",
		"ilptask": taskName,
	}); err != nil {
		return err
	}
	if len(pod.Items) == 0 {
		return nil
	}
	for _, p := range pod.Items {
		if err := r.Delete(ctx, &p); err != nil {
			return err
		}
	}
	return nil
}


type computeProfileService struct {
	name    string
	cpu     float64
	ram     float64
	usedCPU float64
	usedRAM float64
	gpuEnabled bool
	gpuManufacturer string
	deployCost     float64
}

// calculateMinComputeResource returns the minimum compute resource required for a deployment
// based on all its containers' resources
func (r *ILPTaskReconciler) calculateMinComputeResource(ns string, deployName string) (*computeProfileService, error) {
	// We proceed with structured objects for simplicity instead of
	// unsctructured objects
	depObj := &appsv1.Deployment{}
	if err := r.Get(context.TODO(), client.ObjectKey{
		Namespace: ns, Name: deployName,
	}, depObj); err != nil {
		return nil, errors.Wrap(err, "error getting deployment: " + deployName)
	}
	// Each deployment has a single pod but may contain multiple containers
	// For each deployment (and subsequently each pod) we dervie the
	// total cpu and memory for all its containers
	allContainers := []corev1.Container{}
	allContainers = append(allContainers, depObj.Spec.Template.Spec.Containers...)
	allContainers = append(allContainers, depObj.Spec.Template.Spec.InitContainers...)
	totalCPU, totalMem := 0.0, 0.0
	for _, container := range allContainers {
		cpu, mem := getContainerComputeResources(container)
		totalCPU += cpu
		totalMem += mem
	}
	return &computeProfileService{name: deployName, cpu: totalCPU, ram: totalMem}, nil
}