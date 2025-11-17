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

	// "slices"
	// "strings"
	// "time"

	// appsv1 "k8s.io/api/apps/v1"
	// corev1 "k8s.io/api/core/v1"

	// "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	// "k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	// errors2 "errors"

	// "github.com/pkg/errors"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	// hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	// pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	// sv1a1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
)

// The reconciler reads a SkyCluster and first validates that both DataflowPolicyRef and DeploymentPolciyRef are set and have the same name as each other and as the SkyCluster; if not it marks the SkyCluster unready and stops.

// Intended overall flow (most of which is currently commented out):

// Read DeploymentPolicy and DataflowPolicy, plus ConfigMaps that describe "flavors".
// For each entry in the deployment policy, fetch the referenced component (Deployment or other CR) as an unstructured object, extract resource/location constraints and "virtual services", and append a corresponding SkyComponent entry to SkyCluster.Spec.SkyComponents.
// Create an ILPTask owned by the SkyCluster to run an optimization over the SkyComponents; ILPTask updates SkyCluster.Status with a deployment plan when finished.
// If the ILPTask result is "Optimal", generate manifests (deploy map) and create/own Atlas and SkyApp objects containing those manifests to drive the actual deployment, then set SkyCluster ready.
// Resource relationships:

// SkyCluster references DeploymentPolicy and DataflowPolicy by name (must match).
// SkyCluster becomes owner/controller of ILPTask, and (on success) of Atlas and SkyApp.
// It reads/links to cluster objects (Deployments and other components) via ObjectReferences inside SkyCluster.Spec.SkyComponents.
// Uses ConfigMaps (flavor definitions) to match resource requirements to flavors.
// Current effective behavior: only the initial validation and condition-setting run; the optimization/creation logic is present but commented out.

// type computeResource struct {
// 	name    string
// 	cpu     float64
// 	ram     float64
// 	usedCPU float64
// 	usedRAM float64
// }

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

	skyCluster := &cv1a1.SkyCluster{}
	if err := r.Get(ctx, req.NamespacedName, skyCluster); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t SkyCluster not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	
	// Get all uniqe flavors from configmaps and store them
	// allFlavors := getUniqueFlavors(allConfigMap)
	// logger.Info(fmt.Sprintf("[%s]\t Flavors [%d] found.", loggerName, len(allFlavors)))


	// 	objVServices := make([]hv1a1.VirtualService, 0)
	// 	if dp.ComponentRef.Kind == "Deployment" {
	// 		minCPU, minRAM, err := r.calculateDeploymentResources(ctx, req, dp.ComponentRef.Name)
	// 		if err != nil {
	// 			logger.Info(fmt.Sprintf("[%s]\t Error getting minimum flavor for pod.", loggerName))
	// 			r.setConditionUnreadyAndUpdate(skyCluster, "Error getting minimum flavor for pod.")
	// 			return ctrl.Result{}, err
	// 		}
	// 		logger.Info(fmt.Sprintf("[%s]\t Minimum Flavor for [%s] is [%F vCPU, %fGB].", loggerName, dp.ComponentRef.Name, minCPU, minRAM))

			// Select all flavors that satisfy the requirements
	// 		okFlavors, err := getCompatibleFlavors(minCPU, minRAM, allFlavors)
	// 		if err != nil {
	// 			logger.Info(fmt.Sprintf("[%s]\t Error getting proper flavors for pod.", loggerName))
	// 			r.setConditionUnreadyAndUpdate(skyCluster, "Error getting proper flavors for pod.")
	// 			return ctrl.Result{}, client.IgnoreNotFound(err)
	// 		}
	// 		objVServices = append(objVServices, hv1a1.VirtualService{
	// 			Name: strings.Join(okFlavors, "__"), Type: "skyvm_flavor"})
	// 	} else {
	// 		nestedObj, err := GetNestedField(obj.Object, "spec")
	// 		if err != nil {
	// 			logger.Info(fmt.Sprintf("[%s]\t Error getting nested field.", loggerName))
	// 			r.setConditionUnreadyAndUpdate(skyCluster, "Error getting nested field.")
	// 			return ctrl.Result{}, err
	// 		}
	// 		for nestedField, nestedFieldValue := range nestedObj {
				// Check if these fields is considered a "virtual service"
				// TODO: Potentially a better system for managing the virtual services
				// relationship and dependencies can be implemented, but for now
				// we just copy whatever fields in the Spec field of the components
				// that is not empty, (e.g. flavor, image, etc. for SkyVM)
	// 			if slices.Contains(cv1a1.SkyVMVirtualServices, nestedField) {
					// Construct virtual service name (e.g. skyvm_flavor_2vCPU-4GB)
	// 				objVservice := fmt.Sprintf(
	// 					"%s|1", nestedFieldValue.(string))
	// 				objVServices = append(objVServices, hv1a1.VirtualService{
	// 					Name: objVservice,
	// 					Type: fmt.Sprintf("%s_%s", strings.ToLower(dp.ComponentRef.Kind), nestedField)})
	// 			}
	// 		}
	// 	}

	
	return ctrl.Result{}, nil
}

// calculateDeploymentMinResources returns the minimum resource required for a deployment
// based on the limits and requests of all its containers
// func (r *SkyClusterReconciler) calculateDeploymentResources(ctx context.Context, req ctrl.Request, deployName string) (float64, float64, error) {
// 	// Get the deployment first
// 	deploy := &appsv1.Deployment{}
// 	if err := r.Get(ctx, client.ObjectKey{
// 		Namespace: req.Namespace, Name: deployName,
// 	}, deploy); err != nil {
// 		return 0.0, 0.0, err
// 	}
// 	// Get the pod template
// 	podTemplate := deploy.Spec.Template
// 	// Get the containers
// 	containers := []corev1.Container{}
// 	containers = append(containers, podTemplate.Spec.Containers...)
// 	containers = append(containers, podTemplate.Spec.InitContainers...)
// 	// Check each container
// 	cpus := make([]float64, 0)
// 	mems := make([]float64, 0)
// 	for _, container := range containers {
// 		cpu, mem := getContainerComputeResources(container)
// 		cpus = append(cpus, cpu)
// 		mems = append(mems, mem)
// 	}
// 	// across all containers, get the maximum of all request and limits for both cpu and memory
// 	// This would be the minimum flavor required for the deployment
// 	minCPU := max(slices.Max(cpus), 1)
// 	minRAM := max(slices.Max(mems), 2)
// 	return minCPU, minRAM, nil
// }

// calculateMinComputeResource returns the minimum compute resource required for a deployment
// based on all its containers' resources
// func (r *SkyClusterReconciler) calculateMinComputeResource(ctx context.Context, req ctrl.Request, deployName string) (*computeResource, error) {
// 	// We proceed with structured objects for simplicity instead of
// 	// unsctructured objects
// 	depObj := &appsv1.Deployment{}
// 	if err := r.Get(ctx, client.ObjectKey{
// 		Namespace: req.Namespace, Name: deployName,
// 	}, depObj); err != nil {
// 		return nil, errors.Wrap(err, fmt.Sprintf("Error getting deployment [%s].", deployName))
// 	}
// 	// Each deployment has a single pod but may contain multiple containers
// 	// For each deployment (and subsequently each pod) we dervie the
// 	// total cpu and memory for all its containers
// 	allContainers := []corev1.Container{}
// 	allContainers = append(allContainers, depObj.Spec.Template.Spec.Containers...)
// 	allContainers = append(allContainers, depObj.Spec.Template.Spec.InitContainers...)
// 	totalCPU, totalMem := 0.0, 0.0
// 	for _, container := range allContainers {
// 		cpu, mem := getContainerComputeResources(container)
// 		totalCPU += cpu
// 		totalMem += mem
// 	}
// 	return &computeResource{name: deployName, cpu: totalCPU, ram: totalMem}, nil
// }

// func (r *SkyClusterReconciler) generateSkyVMManifest(ctx context.Context, req ctrl.Request, component hv1a1.SkyService) (*hv1a1.SkyService, error) {
// 	cmpntName := component.ComponentRef.Name
// 	xrdObj := &unstructured.Unstructured{}
// 	xrdObj.SetAPIVersion("xrds.skycluster.io/v1alpha1")
// 	// Should be the same as SkyObj, here it should be SkyVM
// 	xrdObj.SetKind(component.ComponentRef.Kind)
// 	xrdObj.SetNamespace(req.Namespace)
// 	// TODO: There may be issues with names containing "."
// 	// and we keep the original name in the labels
// 	// Also, the Atlas object contains the original name
// 	xrdObj.SetName(component.ComponentRef.Name)


// func (r *SkyClusterReconciler) createXRDs(ctx context.Context, req ctrl.Request, deployMap hv1a1.SkyService) ([]hv1a1.SkyService, []hv1a1.SkyService, error) {

// 	// ######### Deployments
// 	allDeployments := make([]hv1a1.SkyService, 0)
// 	for _, deployItem := range deployMap.Component {
// 		// For each component we check its kind and based on that we decide how to proceed:
// 		// 	If this is a Sky Service, then we create the corresponding Service (maybe just the yaml file?)
// 		// 	If this is a Deployment, then we need to group the deployments based on the provider
// 		// Then using decreasing first fit, we identitfy the number and type of VMs required.
// 		// Then we create SkyK8SCluster with a controller and agents specified in previous step.
// 		// We also need to annotate deployments carefully and create services and istio resources accordingly.

// 		// based on the type of services we may modify the objects' spec
// 		switch strings.ToLower(deployItem.ComponentRef.Kind) {
// 		case "deployment":
// 			// fmt.Printf("[Generate]\t Skipping manifest for Deployment [%s]...\n", deployItem.Component.Name)
// 			allDeployments = append(allDeployments, deployItem)
// 		case "vm":
// 			skyObj, err := r.generateSkyVMManifest(ctx, req, deployItem)
// 			if err != nil {
// 				return nil, nil, errors.Wrap(err, "Error generating SkyVM manifest.")
// 			}
// 			manifests = append(manifests, *skyObj)
// 		default:
// 			// We only support above services for now...
// 			return nil, nil, errors.New(fmt.Sprintf("unsupported component type [%s]. Skipping...\n", deployItem.ComponentRef.Kind))
// 		}
// 	}

// 	// ######### Handle Deployments for SkyK8SCluster
// 	skyK8SObj, err := r.generateSkyK8SCluster(ctx, req, allDeployments)
// 	if err != nil {
// 		return nil, nil, errors.Wrap(err, "Error generating SkyK8SCluster.")
// 	}
// 	manifests = append(manifests, *skyK8SObj)

// 	// In addition to K8S cluster manfiests, we also generate application manifests
// 	// (i.e. deployments, services, istio configurations, etc.) and
// 	// submit them to the remote cluster using Kubernetes Provider (object)
// 	// We create manifest and submit it to the SkyAPP controller for further processing
// 	appManifests, err := r.generateSkyAppManifests(ctx, req, deployMap)
// 	if err != nil {
// 		return nil, nil, errors.Wrap(err, "Error generating SkyApp manifests.")
// 	}

// 	return manifests, appManifests, nil
// }

// SetupWithManager sets up the controller with the Manager.
func (r *SkyClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.SkyCluster{}).
		Named("core-skycluster").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}
