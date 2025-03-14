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
SkyCluster is a custom resource that defines the desired state of SkyCluster per application
which contains the deployment reference and data flow reference.
Additionally it contains all selected providers and their configurations.
SkyCluster also contains the application components, including the deployments
and all sky components (e.g. SkyVM, SkyDB, SkyStorage, SkyNetwork, etc.)

Once the dataflow and deployment policies are created, the SkyCluster controller
reconciler get all deployments and sky components label with skycluster.io
and check their requirements and set their minimum flavor (for deployments)
and location constraints (for deployments and all other services) as annotations.

It then creates the ILPTask and waits for the optimization to finish. Once the
optimization is succeeded, it creates the deployment plan and sets the SkyProvider
in spec field which results in the creation of SkyProviders, consequently the
Sky Services and SkyK8SCluster for the application.

The SkyCluster includes list of all deployments and SkyCluster as its spec.skyComponents
and uses this list to create the ILPTask and later to deploy services.
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	policyv1alpha1 "github.com/etesami/skycluster-manager/api/policy/v1alpha1"
	"github.com/pkg/errors"
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

	// ########### ########### ########### ########### ###########
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

	// ########### ########### ########### ########### ###########
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
			if !strings.Contains(k, "skyvm_flavor") {
				continue
			}
			flavorName := strings.Split(k, "_")[2]
			if _, ok := allFlavors_set[flavorName]; ok {
				continue
			}
			allFlavors = append(allFlavors, flavorName)
			allFlavors_set[flavorName] = struct{}{}
		}
	}
	logger.Info(fmt.Sprintf("[%s]\t Flavors [%d] found.", loggerName, len(allFlavors)))

	// ########### ########### ########### ########### ###########
	// List all deployments that have skycluster labels
	allDeploy := &appsv1.DeploymentList{}
	if err := r.List(ctx, allDeploy, client.MatchingLabels{
		corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL: corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
	}); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error listing SkyApps.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info(fmt.Sprintf("[%s]\t Deployments [%d] found.", loggerName, len(allDeploy.Items)))

	// ########### ########### ########### ########### ###########
	// Check each deployments' pod template and check its cpu and memory limits, and request, then
	// Comparing the requirements with flavors, we select the minimum flavor that satisfies the requirements
	deploysLocReqConstraints := make(map[string][]corev1alpha1.ProviderRefSpec)
	deploysLocPerConstraints := make(map[string][]corev1alpha1.ProviderRefSpec)
	for _, deploy := range allDeploy.Items {
		// the deployment name should be the same as the component name in the deployment policy
		// if not, we ignore the deployment
		if !deployExistsInDeploymentPolicy(deploy.GetObjectMeta().GetName(), dpPolicy) {
			continue
		}
		minCPU, minRAM := getPodMinimumFlavor(deploy)
		logger.Info(fmt.Sprintf("[%s]\t Minimum Flavor for [%s] is [%dvCPU-%dGB].", loggerName, deploy.GetObjectMeta().GetName(), minCPU, minRAM))

		// Select all flavors that satisfy the requirements
		okFlavors, err := getProperFlavorsForPod(minCPU, minRAM, allFlavors)
		if err != nil {
			logger.Info(fmt.Sprintf("[%s]\t Error getting proper flavors for pod.", loggerName))
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		// Set the deployment as a key and the location constraints as value
		k := deploy.GetObjectMeta().GetName() + "." + deploy.GetObjectKind().GroupVersionKind().Kind

		// Get the location policies of each deployment
		// Using the name of the deployment, we search through dpPolicy
		for _, dp := range dpPolicy.Spec.DeploymentPolicies {
			// If the deployment name matches the deploymentRef name we check its location constraints
			if dp.ComponentRef.Name == deploy.GetObjectMeta().GetName() {
				locs_permitted := make([]corev1alpha1.ProviderRefSpec, 0)
				for _, loc := range dp.LocationConstraint.Permitted {
					locs_permitted = append(locs_permitted, corev1alpha1.ProviderRefSpec{
						ProviderName:   loc.Name,
						ProviderType:   loc.Type,
						ProviderRegion: loc.Region,
						ProviderZone:   loc.Zone,
					})
				}
				locs_required := make([]corev1alpha1.ProviderRefSpec, 0)
				for _, loc := range dp.LocationConstraint.Required {
					locs_required = append(locs_required, corev1alpha1.ProviderRefSpec{
						ProviderName:   loc.Name,
						ProviderType:   loc.Type,
						ProviderRegion: loc.Region,
						ProviderZone:   loc.Zone,
					})
				}
				deploysLocPerConstraints[k] = locs_permitted
				deploysLocReqConstraints[k] = locs_required
			}
		}

		// We also add the reference to this deployment to the SkyCluster spec.skyComponents
		// The provider field is not set as it will be set by the optimizer
		skyCluster.Spec.SkyComponents = append(
			skyCluster.Spec.SkyComponents, corev1alpha1.SkyComponent{
				Component: corev1.ObjectReference{
					APIVersion: deploy.APIVersion,
					Kind:       deploy.Kind,
					Namespace:  deploy.Namespace,
					Name:       deploy.Name,
				},
				LocationConstraint: corev1alpha1.LocationConstraint{
					Required:  deploysLocReqConstraints[k],
					Permitted: deploysLocPerConstraints[k],
				},
				VirtualServices: []corev1alpha1.VirtualService{
					{Name: strings.Join(okFlavors, "__"), Type: "skyvm_flavor"},
				},
			})

		// // Update the deployment
		// if err := r.Update(ctx, &deploy); err != nil {
		// 	logger.Info(fmt.Sprintf("[%s]\t Error updating Deployment.", loggerName))
		// 	return ctrl.Result{}, client.IgnoreNotFound(err)
		// }
	}

	// ########### ########### ########### ########### ###########
	// In addition to deployments, there may be other Sky Services referenced in
	// DeploymentPolicy object. We need to check them as well and include them in the optimization.
	for _, dp := range dpPolicy.Spec.DeploymentPolicies {
		if dp.ComponentRef.Kind == "Deployment" {
			// This is a deployment, we have already processed it
			continue
		}
		// Get the object's performance and location constraints
		gv, err := schema.ParseGroupVersion(dp.ComponentRef.APIVersion)
		if err != nil {
			logger.Info(fmt.Sprintf("[%s]\t Error parsing APIVersion.", loggerName))
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		gk := schema.GroupVersionKind{
			Group:   gv.Group,
			Version: gv.Version,
			Kind:    dp.ComponentRef.Kind,
		}
		// Get the object
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gk)
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: req.Namespace,
			Name:      dp.ComponentRef.Name,
		}, obj); err != nil {
			logger.Info(
				fmt.Sprintf(
					"[%s]\t Object not found: Name: [%s], Kind: [%s].",
					loggerName, dp.ComponentRef.Name, dp.ComponentRef.Kind))
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		logger.Info(fmt.Sprintf("[%s]\t Object found.", loggerName))

		locs_permitted := make([]corev1alpha1.ProviderRefSpec, 0)
		locs_required := make([]corev1alpha1.ProviderRefSpec, 0)
		for _, loc := range dp.LocationConstraint.Permitted {
			locs_permitted = append(locs_permitted, corev1alpha1.ProviderRefSpec{
				ProviderName:   loc.Name,
				ProviderType:   loc.Type,
				ProviderRegion: loc.Region,
				ProviderZone:   loc.Zone,
			})
		}
		for _, loc := range dp.LocationConstraint.Required {
			locs_required = append(locs_required, corev1alpha1.ProviderRefSpec{
				ProviderName:   loc.Name,
				ProviderType:   loc.Type,
				ProviderRegion: loc.Region,
				ProviderZone:   loc.Zone,
			})
		}

		objVServices := make([]corev1alpha1.VirtualService, 0)
		nestedObj, err := GetNestedField(obj.Object, "spec")
		if err != nil {
			logger.Info(fmt.Sprintf("[%s]\t Error getting nested field.", loggerName))
			return ctrl.Result{}, err
		}
		for nestedField, nestedFieldValue := range nestedObj {
			// Check if these fields is considered a "virtual service"
			if slices.Contains(corev1alpha1.SkyVMVirtualServices, nestedField) {
				// Construct virtual service name (e.g. skyvm_flavor_2vCPU-4GB)
				objVservice := fmt.Sprintf(
					"%s|1", nestedFieldValue.(string))
				objVServices = append(objVServices, corev1alpha1.VirtualService{
					Name: objVservice,
					Type: fmt.Sprintf("%s_%s", strings.ToLower(dp.ComponentRef.Kind), nestedField)})
			}
		}

		// We also add the reference to this object to the SkyCluster spec.skyComponents
		// The provider field is not set as it will be set by the optimizer
		skyCluster.Spec.SkyComponents = append(
			skyCluster.Spec.SkyComponents, corev1alpha1.SkyComponent{
				Component: corev1.ObjectReference{
					APIVersion: obj.GetAPIVersion(),
					Kind:       obj.GetKind(),
					Namespace:  obj.GetNamespace(),
					Name:       obj.GetName(),
				},
				LocationConstraint: corev1alpha1.LocationConstraint{
					Required:  locs_required,
					Permitted: locs_permitted,
				},
				// TODO: Potentially a better system for managing the virtual services
				// relationship and dependencies can be implemented, but for now
				// we just copy whatever fields in the Spec field of the components
				// that is not empty, (e.g. flavor, image, etc. for SkyVM)
				VirtualServices: objVServices,
			})

		// // Update the object
		// if err := r.Update(ctx, obj); err != nil {
		// 	logger.Info(fmt.Sprintf("[%s]\t Error updating Object.", loggerName))
		// 	return ctrl.Result{}, client.IgnoreNotFound(err)
		// }
	}

	// ########### ########### ########### ########### ###########
	// Create the ILPTask object, once the ILPTask is finished,
	// it updates the SkyCluster's status with the deployment plan
	ilpTask := &corev1alpha1.ILPTask{
		ObjectMeta: metav1.ObjectMeta{
			Name:      skyCluster.Name,
			Namespace: skyCluster.Namespace,
		},
		Spec: corev1alpha1.ILPTaskSpec{
			SkyComponents: skyCluster.Spec.SkyComponents,
		},
	}
	if err := ctrl.SetControllerReference(skyCluster, ilpTask, r.Scheme); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error setting owner reference.", loggerName))
		return ctrl.Result{}, err
	}

	// ########### ########### ########### ########### ###########
	// Save the SkyCluster object
	if err := r.Update(ctx, skyCluster); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error updating SkyCluster.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	skyCluster.Status.DeploymentPlanStatus = "Pending"
	// Save the SkyCluster object status
	if err := r.Status().Update(ctx, skyCluster); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error updating SkyCluster.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.Create(ctx, ilpTask); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t Error creating ILPTask.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

func getProperFlavorsForPod(minCPU, minRAM int, allFlavors []string) ([]string, error) {
	okFlavors := make([]string, 0)
	for _, skyFlavor := range allFlavors {
		cpu := strings.Split(skyFlavor, "-")[0]
		cpu = strings.Replace(cpu, "vCPU", "", -1)
		cpu_int, err1 := strconv.Atoi(cpu)
		ram := strings.Split(skyFlavor, "-")[1]
		ram = strings.Replace(ram, "GB", "", -1)
		ram_int, err2 := strconv.Atoi(ram)
		if err1 != nil || err2 != nil {
			if err1 != nil {
				return nil, errors.Wrap(err1, "Error converting flavor spec to int.")
			}
			if err2 != nil {
				return nil, errors.Wrap(err1, "Error converting flavor spec to int.")
			}
			// if there are error processing the flavors we ignore them and not add them to the list
			continue
		}
		if cpu_int >= minCPU && ram_int >= minRAM {
			okFlavors = append(okFlavors, fmt.Sprintf("%s|1", skyFlavor))
		}
	}
	return okFlavors, nil
}

func getPodMinimumFlavor(deploy appsv1.Deployment) (int, int) {
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
	return minCPU, minRAM
}

func getLocationConstraints(dp policyv1alpha1.DeploymentPolicyItem) ([]string, []string) {
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

	locs_required := make([]string, 0)
	for _, loc := range requiredLocations {
		// Name, Type, RegionAlias, Region
		locDetails := loc.Name + "|" + loc.Type + "||" + loc.Region
		locs_required = append(locs_required, locDetails)
	}
	return locs_permitted, locs_required
}

func deployExistsInDeploymentPolicy(deployName string, dpPolicy *policyv1alpha1.DeploymentPolicy) bool {
	for _, dp := range dpPolicy.Spec.DeploymentPolicies {
		if dp.ComponentRef.Name == deployName {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyCluster{}).
		Named("core-skycluster").
		Complete(r)
}
