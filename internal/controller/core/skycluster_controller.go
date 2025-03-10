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
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	res "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	policyv1alpha1 "github.com/etesami/skycluster-manager/api/policy/v1alpha1"
)

// SkyClusterReconciler reconciles a SkyCluster object
type SkyClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyclusters/finalizers,verbs=update

func (r *SkyClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	loggerName := "SkyCluster"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling SkyCluster for [%s]", loggerName, req.Name))

	skyCluster := &corev1alpha1.SkyCluster{}
	if err := r.Get(ctx, req.NamespacedName, skyCluster); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t SkyCluster not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if both DeploymentPolicy and DataflowPolicy are set
	if skyCluster.Spec.DataflowPolicyRef.Name == "" || skyCluster.Spec.DeploymentPolciyRef.Name == "" {
		logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy or DataflowPolicy not set.", loggerName))
		return ctrl.Result{}, nil
	}

	// Check if both DeploymentPolicy and DataflowPolicy are same name
	sameNamePolicies := skyCluster.Spec.DataflowPolicyRef.Name == skyCluster.Spec.DeploymentPolciyRef.Name
	sameName := skyCluster.Spec.DataflowPolicyRef.Name
	if !sameNamePolicies {
		logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy and DataflowPolicy are not same name.", loggerName))
		return ctrl.Result{}, nil
	}

	// Check if DeploymentPolicy and DataflowPolicy are same name with SkyCluster
	sameNameAll := sameName == skyCluster.GetObjectMeta().GetName()
	if !sameNameAll {
		logger.Info(fmt.Sprintf("[%s]\t Different name with DeploymentPolicy and DataflowPolicy.", loggerName))
		return ctrl.Result{}, nil
	}

	// We are all good, let's check the deployment plan
	// If we already have the deployment plan, no need to make any changes
	if skyCluster.Status.DeploymentPlanStatus != "" || skyCluster.Status.DeploymentPlan != "" {
		logger.Info(fmt.Sprintf("[%s]\t DeploymentPlan/Status already exists.", loggerName))
		return ctrl.Result{}, nil
	}

	// Get dataflow and deployment policies as we need them later
	dfPolicy := &policyv1alpha1.DataflowPolicy{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      skyCluster.Spec.DataflowPolicyRef.Name,
	}, dfPolicy); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t DataflowPolicy not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info(fmt.Sprintf("[%s]\t DataflowPolicy found.", loggerName))

	dpPolicy := &policyv1alpha1.DeploymentPolicy{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      skyCluster.Spec.DeploymentPolciyRef.Name,
	}, dpPolicy); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy found.", loggerName))

	// Get all configmap with skycluster labels to store flavor sizes
	// We will use flavors to specify requirements for each deployment
	allConfigMap := &corev1.ConfigMapList{}
	if err := r.List(ctx, allConfigMap, client.MatchingLabels{
		corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL:  corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
		corev1alpha1.SKYCLUSTER_CONFIGTYPE_LABEL: corev1alpha1.SKYCLUSTER_VSERVICES_LABEL,
	}); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error listing ConfigMaps.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info(fmt.Sprintf("[%s]\t ConfigMaps [%d] found.", loggerName, len(allConfigMap.Items)))

	// Get all uniqe flavors from configmaps and store them
	allFlavors := make([]string, 0)
	allFlavors_set := make(map[string]struct{}, 0)
	for _, cm := range allConfigMap.Items {
		for k := range cm.Data {
			if !strings.Contains(k, "skyvm.flavor") {
				continue
			}
			flavorName := strings.Split(k, ".")[2]
			if _, ok := allFlavors_set[flavorName]; ok {
				continue
			}
			allFlavors = append(allFlavors, flavorName)
			allFlavors_set[flavorName] = struct{}{}
		}
	}
	logger.Info(fmt.Sprintf("[%s]\t Flavors [%d] found.", loggerName, len(allFlavors)))

	// List all deployments that have skycluster labels
	allDeploy := &appsv1.DeploymentList{}
	if err := r.List(ctx, allDeploy, client.MatchingLabels{
		corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL: corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
	}); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error listing SkyApps.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info(fmt.Sprintf("[%s]\t Deployments [%d] found.", loggerName, len(allDeploy.Items)))

	// Check each deployments' pod template and check its cpu and memory limits, and request, then
	// Comparing the requirements with flavors, we select the minimum flavor that satisfies the requirements

	for _, deploy := range allDeploy.Items {

		// Get the pod template
		podTemplate := deploy.Spec.Template
		// Get the containers
		containers := podTemplate.Spec.Containers
		// Check each container
		minimumFlavorCPU := make([]int, len(containers))
		minimumFlavorRAM := make([]int, len(containers))
		for _, container := range containers {
			// Get the resources
			resources := container.Resources
			// Get the limits
			limits := resources.Limits
			// Get the requests
			requests := resources.Requests
			// Check the limits

			for k, v := range limits {
				if strings.Contains(k.String(), "cpu") {
					if v.Cmp(*res.NewMilliQuantity(1000, res.DecimalSI)) == 0 {
						minimumFlavorCPU = append(minimumFlavorCPU, 1)
					} else if v.Cmp(*res.NewMilliQuantity(2000, res.DecimalSI)) == 0 {
						minimumFlavorCPU = append(minimumFlavorCPU, 2)
					} else if v.Cmp(*res.NewMilliQuantity(4000, res.DecimalSI)) == 0 {
						minimumFlavorCPU = append(minimumFlavorCPU, 4)
					}
				}
				if strings.Contains(k.String(), "memory") {
					if v.Cmp(*res.NewQuantity(1<<30, res.BinarySI)) == 0 {
						minimumFlavorRAM = append(minimumFlavorRAM, 1)
					} else if v.Cmp(*res.NewQuantity(2<<30, res.BinarySI)) == 0 {
						minimumFlavorRAM = append(minimumFlavorRAM, 2)
					} else if v.Cmp(*res.NewQuantity(4<<30, res.BinarySI)) == 0 {
						minimumFlavorRAM = append(minimumFlavorRAM, 4)
					}
				}
			}
			// Check the requests
			for k, v := range requests {
				if strings.Contains(k.String(), "cpu") {
					if v.Cmp(*res.NewMilliQuantity(1000, res.DecimalSI)) == 0 {
						minimumFlavorCPU = append(minimumFlavorCPU, 1)
					} else if v.Cmp(*res.NewMilliQuantity(2000, res.DecimalSI)) == 0 {
						minimumFlavorCPU = append(minimumFlavorCPU, 2)
					} else if v.Cmp(*res.NewMilliQuantity(4000, res.DecimalSI)) == 0 {
						minimumFlavorCPU = append(minimumFlavorCPU, 4)
					}
				}
				if strings.Contains(k.String(), "memory") {
					if v.Cmp(*res.NewQuantity(1<<30, res.BinarySI)) == 0 {
						minimumFlavorRAM = append(minimumFlavorRAM, 1)
					} else if v.Cmp(*res.NewQuantity(2<<30, res.BinarySI)) == 0 {
						minimumFlavorRAM = append(minimumFlavorRAM, 2)
					} else if v.Cmp(*res.NewQuantity(4<<30, res.BinarySI)) == 0 {
						minimumFlavorRAM = append(minimumFlavorRAM, 4)
					}
				}
			}
		}
		// across all containers, get the maximum of all request and limits for both cpu and memory
		// This would be the minimum flavor required for the deployment
		minCPU := max(slices.Max(minimumFlavorCPU), 1)
		minRAM := max(slices.Max(minimumFlavorRAM), 2)
		logger.Info(fmt.Sprintf("[%s]\t Minimum Flavor for [%s] is [%dvCPU-%dGB].", loggerName, deploy.GetObjectMeta().GetName(), minCPU, minRAM))

		// Select all flavors that satisfy the requirements
		// We can either dynamically get all available flavors
		// or statically define them in the code: corev1alpha1.SKYCLUSTER_FLAVORS
		okFlavors := make([]string, 0)
		for _, skyFlavor := range allFlavors {
			cpu := strings.Split(skyFlavor, "-")[0]
			cpu = strings.Replace(cpu, "vCPU", "", -1)
			cpu_int, err1 := strconv.Atoi(cpu)
			ram := strings.Split(skyFlavor, "-")[1]
			ram = strings.Replace(ram, "GB", "", -1)
			ram_int, err2 := strconv.Atoi(ram)
			if err1 != nil || err2 != nil {
				logger.Info(fmt.Sprintf("[%s]\t Error converting flavor spec to int.", loggerName))
				if err1 != nil {
					logger.Error(err1, "Error converting flavor spec to int.")
				}
				if err2 != nil {
					logger.Error(err2, "Error converting flavor spec to int.")
				}
				// if there are error processing the flavors we ignore them and not add them to the list
				continue
			}
			if cpu_int >= minCPU && ram_int >= minRAM {
				okFlavors = append(okFlavors, skyFlavor)
			}
		}
		// Set this minimum flavor to the deployment as an annotation
		deploy.ObjectMeta.Annotations[corev1alpha1.SKYCLUSTER_API+"/skyvm-flavor"] = strings.Join(okFlavors, ",")

		// Get the location policies of each deployment
		// Using the name of the deployment, we search through dpPolicy
		for _, dp := range dpPolicy.Spec.DeploymentPolicies {
			// if the deployment name matches the deploymentRef name we check its location constraints
			if dp.DeploymentRef.Name == deploy.GetObjectMeta().GetName() {
				// Get the location constraints
				locationConstraints := dp.LocationConstraint
				// Get the permitted locations
				permittedLocations := locationConstraints.Permitted
				// Get the required locations
				requiredLocations := locationConstraints.Required
				// Set the permitted and required locations as annotations
				locs_permitted := make([]string, 0)
				for _, loc := range permittedLocations {
					// Name, Type, RegionAlias, Region
					locDetails := loc.Name + "|" + loc.Type + "||" + loc.Region
					locs_permitted = append(locs_permitted, locDetails)
				}
				logger.Info(fmt.Sprintf("[%s]\t Permitted Locations [%s].", loggerName, strings.Join(locs_permitted, "__")))
				deploy.ObjectMeta.Annotations[corev1alpha1.SKYCLUSTER_API+"/skyvm-permitted-locations"] = strings.Join(locs_permitted, "__")
				locs_required := make([]string, 0)
				for _, loc := range requiredLocations {
					// Name, Type, RegionAlias, Region
					locDetails := loc.Name + "|" + loc.Type + "||" + loc.Region
					locs_required = append(locs_required, locDetails)
				}
				logger.Info(fmt.Sprintf("[%s]\t Required Locations [%s].", loggerName, strings.Join(locs_required, "__")))
				deploy.ObjectMeta.Annotations[corev1alpha1.SKYCLUSTER_API+"/skyvm-required-locations"] = strings.Join(locs_required, "__")
			}
		}

		// Update the deployment
		if err := r.Update(ctx, &deploy); err != nil {
			logger.Info(fmt.Sprintf("[%s]\t Error updating Deployment.", loggerName))
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyCluster{}).
		Named("core-skycluster").
		Complete(r)
}
