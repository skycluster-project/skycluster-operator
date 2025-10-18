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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// If the ILPTask result is "Optimal", generate manifests (deploy map) and create/own SkyXRD and SkyApp objects containing those manifests to drive the actual deployment, then set SkyCluster ready.
// Resource relationships:

// SkyCluster references DeploymentPolicy and DataflowPolicy by name (must match).
// SkyCluster becomes owner/controller of ILPTask, and (on success) of SkyXRD and SkyApp.
// It reads/links to cluster objects (Deployments and other components) via ObjectReferences inside SkyCluster.Spec.SkyComponents.
// Uses ConfigMaps (flavor definitions) to match resource requirements to flavors.
// Current effective behavior: only the initial validation and condition-setting run; the optimization/creation logic is present but commented out.



type computeResource struct {
	name    string
	cpu     float64
	ram     float64
	usedCPU float64
	usedRAM float64
}

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

	skyCluster.SetCondition("Synced", metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")

	// // ########### ########### ########### ########### ###########
	// // Check if both DeploymentPolicy and DataflowPolicy are set
	// if skyCluster.Spec.DataflowPolicyRef.Name == "" || skyCluster.Spec.DeploymentPolciyRef.Name == "" {
	// 	logger.Info(fmt.Sprintf("[%s]\t DeploymentPolicy or DataflowPolicy not set.", loggerName))
	// 	m := "DeploymentPolicy or DataflowPolicy not set."
	// 	r.setConditionUnreadyAndUpdate(skyCluster, m)
	// 	return ctrl.Result{}, nil
	// }

	// // Check if both DeploymentPolicy and DataflowPolicy are same name
	// sameNamePolicies := skyCluster.Spec.DataflowPolicyRef.Name == skyCluster.Spec.DeploymentPolciyRef.Name
	// sameName := skyCluster.Spec.DataflowPolicyRef.Name
	// if !sameNamePolicies {
	// 	m := "DeploymentPolicy and DataflowPolicy are not same name"
	// 	logger.Info(fmt.Sprintf("[%s]\t[%s].", m, loggerName))
	// 	r.setConditionUnreadyAndUpdate(skyCluster, m)
	// 	return ctrl.Result{}, nil
	// }

	// // Check if DeploymentPolicy and DataflowPolicy are same name with SkyCluster
	// sameNameAll := sameName == skyCluster.GetObjectMeta().GetName()
	// if !sameNameAll {
	// 	m := "DeploymentPolicy and DataflowPolicy are not same name with SkyCluster"
	// 	logger.Info(fmt.Sprintf("[%s]\t[%s].", m, loggerName))
	// 	r.setConditionUnreadyAndUpdate(skyCluster, m)
	// 	return ctrl.Result{}, nil
	// }

	// // ########### ########### ########### ########### ###########
	// // We are all good, let's check the deployment plan
	// // If we already have the deployment plan, no need to make any changes
	// // The Result can be "Optimal" if the optimization is successful
	// // and anything else if it is not successful.
	// if skyCluster.Status.Optimization.Result != "" {
	// 	logger.Info(fmt.Sprintf("[%s]\t DeploymentPlan/Status already exists.", loggerName))
	// 	// We can now proceed with the deployment by creating SkyXRD object.
	// 	if skyCluster.Status.Optimization.Result == "Optimal" {
	// 		logger.Info(fmt.Sprintf("[%s]\t ILPTask is succeeded. Ready to create SkyXRD to initate the deployment.", loggerName))

	// 		manifests, appManifests, err := r.createXRDs(ctx, req, skyCluster.Status.Optimization.DeployMap)
	// 		if err != nil {
	// 			logger.Info(fmt.Sprintf("[%s]\t Error creating SkyXRD.", loggerName))
	// 			r.setConditionUnreadyAndUpdate(skyCluster, "Error creating SkyXRD.")
	// 			return ctrl.Result{}, err
	// 		}

	// 		// if the manifests are generated, we then create or update the SkyXRDs with the complete manifests
	// 		// and let it oversee the deployment process.
	// 		logger.Info(fmt.Sprintf("[%s]\t SkyXRDs manifests created successfully. Len: [%d]", loggerName, len(manifests)))
	// 		// Load SkyXRD and create/update it
	// 		skyXRD := &cv1a1.SkyXRD{}
	// 		if err := r.Get(ctx, req.NamespacedName, skyXRD); err != nil {
	// 			// Create the SkyXRD object
	// 			skyXRD = &cv1a1.SkyXRD{
	// 				ObjectMeta: metav1.ObjectMeta{Name: skyCluster.Name, Namespace: skyCluster.Namespace},
	// 				Spec:       cv1a1.SkyXRDSpec{Manifests: manifests},
	// 			}
	// 			if err := ctrl.SetControllerReference(skyCluster, skyXRD, r.Scheme); err != nil {
	// 				logger.Info(fmt.Sprintf("[%s]\t Error setting owner reference.", loggerName))
	// 				r.setConditionUnreadyAndUpdate(skyCluster, "Error setting owner reference (skyXRD).")
	// 				return ctrl.Result{}, err
	// 			}
	// 			if err := r.Create(ctx, skyXRD); err != nil {
	// 				logger.Info(fmt.Sprintf("[%s]\t Error creating SkyXRD.", loggerName))
	// 				r.setConditionUnreadyAndUpdate(skyCluster, "Error creating SkyXRD.")
	// 				return ctrl.Result{}, err
	// 			}
	// 			logger.Info(fmt.Sprintf("[%s]\t SkyXRD created successfully.", loggerName))
	// 			skyCluster.SetConditionReady()
	// 		} else {
	// 			logger.Info(fmt.Sprintf("[%s]\t SkyXRD already exists. Updating an existing plan is not supported yet.", loggerName))
	// 		}

	// 		skyApp := &sv1a1.SkyApp{}
	// 		if err := r.Get(ctx, req.NamespacedName, skyApp); err != nil {
	// 			// Create the SkyApp object
	// 			skyApp = &sv1a1.SkyApp{
	// 				ObjectMeta: metav1.ObjectMeta{Name: skyCluster.Name, Namespace: skyCluster.Namespace},
	// 				Spec:       sv1a1.SkyAppSpec{Manifests: appManifests},
	// 			}
	// 			if err := ctrl.SetControllerReference(skyCluster, skyApp, r.Scheme); err != nil {
	// 				logger.Info(fmt.Sprintf("[%s]\t Error setting owner reference.", loggerName))
	// 				r.setConditionUnreadyAndUpdate(skyCluster, "Error setting owner reference (skyApp).")
	// 				return ctrl.Result{}, err
	// 			}
	// 			if err := r.Create(ctx, skyApp); err != nil {
	// 				logger.Info(fmt.Sprintf("[%s]\t Error creating SkyApp.", loggerName))
	// 				r.setConditionUnreadyAndUpdate(skyCluster, "Error creating SkyApp.")
	// 				return ctrl.Result{}, err
	// 			}
	// 			logger.Info(fmt.Sprintf("[%s]\t SkyApp created successfully.", loggerName))
	// 			skyCluster.SetConditionReady()
	// 		} else {
	// 			logger.Info(fmt.Sprintf("[%s]\t SkyApp already exists. Updating an existing plan is not supported yet.", loggerName))
	// 		}

	// 		if err := r.Status().Update(ctx, skyCluster); err != nil {
	// 			logger.Info(fmt.Sprintf("[%s]\t Error updating SkyCluster status.", loggerName))
	// 			return ctrl.Result{}, err
	// 		}
	// 		return ctrl.Result{}, nil
	// 	}

	// 	if err := r.Status().Update(ctx, skyCluster); err != nil {
	// 		logger.Info(fmt.Sprintf("[%s]\t Error updating SkyCluster status.", loggerName))
	// 		return ctrl.Result{}, err
	// 	}
	// 	return ctrl.Result{}, nil
	// }

	// // If the status is not empty (pending), the ILPTask has been created,
	// // We should wait until the results are avaialble
	// // If the optimization fails, the status will not be updated as the
	// // ILPTask controller will not propagate the status to the SkyCluster
	// // So we wait here for user intervention.
	// if skyCluster.Status.Optimization.Status == "Pending" {
	// 	logger.Info(fmt.Sprintf("[%s]\t ILPTask is pending (or failed). Waiting for the results.", loggerName))
	// 	r.setConditionUnreadyAndUpdate(skyCluster, "ILPTask is pending (or failed). Waiting for the results.")
	// 	return ctrl.Result{}, nil
	// }

	// // If the status is anything else than an empty string, we should not continue.
	// // Something may have gone wrone.
	// if skyCluster.Status.Optimization.Status != "" {
	// 	logger.Info(fmt.Sprintf("[%s]\t ILPTask is not succeeded. Status is [%s]. Please check the status.",
	// 		loggerName, skyCluster.Status.Optimization.Status))
	// 	r.setConditionUnreadyAndUpdate(skyCluster, "ILPTask is not succeeded. Please check the status.")
	// 	return ctrl.Result{}, nil
	// }

	// // We ready to continnue and create the ILPTask for optimization
	// // Get dataflow and deployment policies as we need them later

	// // dfPolicy, err1 := getDFPolicy(r, ctx, req)
	// dpPolicy, err2 := r.getDPPolicy(ctx, req)
	// // Get all configmap with skycluster labels to store flavor sizes
	// // We will use flavors to specify requirements for each deployment
	// allConfigMap, err3 := r.getAllConfigMap(ctx)
	// if err2 != nil || err3 != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t Error getting policies or configmaps.", loggerName))
	// 	r.setConditionUnreadyAndUpdate(skyCluster, "Error getting policies or configmaps.")
	// 	return ctrl.Result{}, errors2.Join(err2, err3)
	// }
	// // if err1 != nil || err2 != nil || err3 != nil {
	// // 	logger.Info(fmt.Sprintf("[%s]\t Error getting policies or configmaps.", loggerName))
	// // 	return ctrl.Result{}, errors2.Join(err1, err2, err3)
	// // }

	// // Get all uniqe flavors from configmaps and store them
	// allFlavors := getUniqueFlavors(allConfigMap)
	// logger.Info(fmt.Sprintf("[%s]\t Flavors [%d] found.", loggerName, len(allFlavors)))

	// // ########### ########### ########### ########### ###########
	// // We list all deployments along with other Sky Services in one go,
	// // and include them in the optimization.
	// for _, dp := range dpPolicy.Spec.DeploymentPolicies {
	// 	// Get the object's performance and location constraints
	// 	gv, err := schema.ParseGroupVersion(dp.ComponentRef.APIVersion)
	// 	if err != nil {
	// 		logger.Info(fmt.Sprintf("[%s]\t Error parsing APIVersion.", loggerName))
	// 		r.setConditionUnreadyAndUpdate(skyCluster, "Error parsing APIVersion.")
	// 		return ctrl.Result{}, client.IgnoreNotFound(err)
	// 	}
	// 	gk := schema.GroupVersionKind{
	// 		Group:   gv.Group,
	// 		Version: gv.Version,
	// 		Kind:    dp.ComponentRef.Kind,
	// 	}
	// 	// Get the object
	// 	obj := &unstructured.Unstructured{}
	// 	obj.SetGroupVersionKind(gk)
	// 	if err := r.Get(ctx, client.ObjectKey{
	// 		Namespace: req.Namespace,
	// 		Name:      dp.ComponentRef.Name,
	// 	}, obj); err != nil {
	// 		m := fmt.Sprintf("[%s]\t Object not found: Name: [%s], Kind: [%s]. Namespace: [%s]",
	// 			loggerName, dp.ComponentRef.Name, dp.ComponentRef.Kind, req.Namespace)
	// 		logger.Info(m)
	// 		r.setConditionUnreadyAndUpdate(skyCluster, m)
	// 		return ctrl.Result{RequeueAfter: 3 * time.Second}, client.IgnoreNotFound(err)
	// 	}
	// 	logger.Info(fmt.Sprintf("[%s]\t Object found.", loggerName))

	// 	objVServices := make([]hv1a1.VirtualService, 0)
	// 	if dp.ComponentRef.Kind == "Deployment" {
	// 		minCPU, minRAM, err := r.calculateDeploymentResources(ctx, req, dp.ComponentRef.Name)
	// 		if err != nil {
	// 			logger.Info(fmt.Sprintf("[%s]\t Error getting minimum flavor for pod.", loggerName))
	// 			r.setConditionUnreadyAndUpdate(skyCluster, "Error getting minimum flavor for pod.")
	// 			return ctrl.Result{}, err
	// 		}
	// 		logger.Info(fmt.Sprintf("[%s]\t Minimum Flavor for [%s] is [%F vCPU, %fGB].", loggerName, dp.ComponentRef.Name, minCPU, minRAM))

	// 		// Select all flavors that satisfy the requirements
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
	// 			// Check if these fields is considered a "virtual service"
	// 			// TODO: Potentially a better system for managing the virtual services
	// 			// relationship and dependencies can be implemented, but for now
	// 			// we just copy whatever fields in the Spec field of the components
	// 			// that is not empty, (e.g. flavor, image, etc. for SkyVM)
	// 			if slices.Contains(cv1a1.SkyVMVirtualServices, nestedField) {
	// 				// Construct virtual service name (e.g. skyvm_flavor_2vCPU-4GB)
	// 				objVservice := fmt.Sprintf(
	// 					"%s|1", nestedFieldValue.(string))
	// 				objVServices = append(objVServices, hv1a1.VirtualService{
	// 					Name: objVservice,
	// 					Type: fmt.Sprintf("%s_%s", strings.ToLower(dp.ComponentRef.Kind), nestedField)})
	// 			}
	// 		}
	// 	}

	// 	// We also add the reference to this object to the SkyCluster spec.skyComponents
	// 	// The provider field is not set as it will be set by the optimizer
	// 	skyCluster.Spec.SkyComponents = append(
	// 		skyCluster.Spec.SkyComponents, hv1a1.SkyComponent{
	// 			Components: corev1.ObjectReference{
	// 				APIVersion: obj.GetAPIVersion(),
	// 				Kind:       obj.GetKind(),
	// 				Namespace:  obj.GetNamespace(),
	// 				Name:       obj.GetName(),
	// 			},
	// 			LocationConstraint: hv1a1.LocationConstraint{
	// 				Required:  dp.LocationConstraint.Required,
	// 				Permitted: dp.LocationConstraint.Permitted,
	// 			},
	// 			VirtualServices: objVServices,
	// 		})
	// }

	// // ########### ########### ########### ########### ###########
	// // Create the ILPTask object, once the ILPTask is finished,
	// // it updates the SkyCluster's status with the deployment plan
	// ilpTask := &cv1a1.ILPTask{
	// 	ObjectMeta: metav1.ObjectMeta{
	// 		Name:      skyCluster.Name,
	// 		Namespace: skyCluster.Namespace,
	// 	},
	// 	Spec: cv1a1.ILPTaskSpec{
	// 		SkyComponents: skyCluster.Spec.SkyComponents,
	// 	},
	// }
	// if err := ctrl.SetControllerReference(skyCluster, ilpTask, r.Scheme); err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t Error setting owner reference.", loggerName))
	// 	return ctrl.Result{}, err
	// }

	// // ########### ########### ########### ########### ###########
	// // Save the SkyCluster object
	// if err := r.Update(ctx, skyCluster); err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t Error updating SkyCluster.", loggerName))
	// 	return ctrl.Result{}, client.IgnoreNotFound(err)
	// }

	// // This is the status of the pod optimization
	// // Upon successful completion, the ILPTask controller
	// // will update the SkyCluster object with the deployment plan
	// // and set the status and result of the optimization accordingly
	// skyCluster.Status.Optimization.Status = "Pending"
	// // Save the SkyCluster object status
	// if err := r.Status().Update(ctx, skyCluster); err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t Error updating SkyCluster.", loggerName))
	// 	r.setConditionUnreadyAndUpdate(skyCluster, "Error updating SkyCluster.")
	// 	return ctrl.Result{}, client.IgnoreNotFound(err)
	// }

	// if err := r.Create(ctx, ilpTask); err != nil {
	// 	logger.Info(fmt.Sprintf("[%s]\t Error creating ILPTask.", loggerName))
	// 	r.setConditionUnreadyAndUpdate(skyCluster, "Error creating ILPTask.")
	// 	return ctrl.Result{}, client.IgnoreNotFound(err)
	// }

	// r.setConditionUnreadyAndUpdate(skyCluster, "ILPTask is created. Waiting for the results.")
	return ctrl.Result{}, nil
}

// func (r *SkyClusterReconciler) setConditionReadyAndUpdate(s *cv1a1.SkyCluster) {
// 	s.SetCondition("Ready", metav1.ConditionTrue, "Available", "SkyCluster is ready.")
// 	if err := r.Status().Update(context.Background(), s); err != nil {
// 		panic(fmt.Sprintf("failed to update SkyCluster status: %v", err))
// 	}
// }

// func (r *SkyClusterReconciler) setConditionUnreadyAndUpdate(s *cv1a1.SkyCluster, m string) {
// 	s.SetCondition("Ready", metav1.ConditionFalse, "Unavailable", m)
// 	if err := r.Status().Update(context.Background(), s); err != nil {
// 		panic(fmt.Sprintf("failed to update SkyCluster status: %v", err))
// 	}
// }

// func (r *SkyClusterReconciler) getDPPolicy(ctx context.Context, req ctrl.Request) (*pv1a1.DeploymentPolicy, error) {
// 	dpPolicy := &pv1a1.DeploymentPolicy{}
// 	if err := r.Get(ctx, client.ObjectKey{
// 		Namespace: req.Namespace, Name: req.Name,
// 	}, dpPolicy); err != nil {
// 		return nil, errors.Wrap(err, "Error getting DeploymentPolicy.")
// 	}
// 	return dpPolicy, nil
// }

// func (r *SkyClusterReconciler) getAllConfigMap(ctx context.Context) (*corev1.ConfigMapList, error) {
// 	allConfigMap := &corev1.ConfigMapList{}
// 	if err := r.List(ctx, allConfigMap, client.MatchingLabels{
// 		hv1a1.SKYCLUSTER_MANAGEDBY_LABEL:  hv1a1.SKYCLUSTER_MANAGEDBY_VALUE,
// 		hv1a1.SKYCLUSTER_CONFIGTYPE_LABEL: hv1a1.SKYCLUSTER_VSERVICES_LABEL,
// 	}); err != nil {
// 		return nil, errors.Wrap(err, "Error listing ConfigMaps.")
// 	}
// 	return allConfigMap, nil
// }

// // getProviderConfigMap returns the ConfigMap for the given provider
// func (r *SkyClusterReconciler) getProviderConfigMap(ctx context.Context, ProviderName, ProviderRegion, ProviderZone string) (*corev1.ConfigMap, error) {
// 	allConfigMap := &corev1.ConfigMapList{}
// 	if err := r.List(ctx, allConfigMap, client.MatchingLabels{
// 		hv1a1.SKYCLUSTER_MANAGEDBY_LABEL:      hv1a1.SKYCLUSTER_MANAGEDBY_VALUE,
// 		hv1a1.SKYCLUSTER_CONFIGTYPE_LABEL:     hv1a1.SKYCLUSTER_VSERVICES_LABEL,
// 		hv1a1.SKYCLUSTER_PROVIDERNAME_LABEL:   ProviderName,
// 		hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL: ProviderRegion,
// 		hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL:   ProviderZone,
// 	}); err != nil {
// 		return nil, errors.Wrap(err, "Error listing ConfigMaps.")
// 	}
// 	if len(allConfigMap.Items) != 1 {
// 		return nil, errors.New(fmt.Sprintf("Error getting ConfigMap. More than one ConfigMap found for [%s], [%s], [%s].", ProviderName, ProviderRegion, ProviderZone))
// 	}
// 	return &allConfigMap.Items[0], nil
// }

// // calculateDeploymentMinResources returns the minimum resource required for a deployment
// // based on the limits and requests of all its containers
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

// // calculateMinComputeResource returns the minimum compute resource required for a deployment
// // based on all its containers' resources
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

// func (r *SkyClusterReconciler) generateProviderManifests(ctx context.Context, req ctrl.Request, components []hv1a1.SkyService) (map[string]hv1a1.SkyService, error) {
// 	// We need to group the components based on the providers
// 	// and then generate the manifests for each provider
// 	// We then return the manifests for each provider
// 	uniqueProviders := make(map[string]hv1a1.ProviderRefSpec, 0)
// 	for _, cmpnt := range components {
// 		// ProviderName uniquely identifies a provider, set by the optimizer
// 		providerName := cmpnt.ProviderRef.ProviderName
// 		if _, ok := uniqueProviders[providerName]; ok {
// 			continue
// 		}
// 		uniqueProviders[providerName] = cmpnt.ProviderRef
// 	}
// 	// Now we have all unique providers
// 	// We can now generate the manifests for each provider
// 	manifests := map[string]hv1a1.SkyService{}
// 	idx := 0
// 	for providerName, provider := range uniqueProviders {
// 		idx += 1
// 		// We need to create the SkyProvider object
// 		// We need to create the SkyK8SCluster
// 		obj := &unstructured.Unstructured{}
// 		obj.SetAPIVersion("xrds.skycluster.io/v1alpha1")
// 		obj.SetKind("Provider")
// 		// Namespace should be in a same namespace as its owner
// 		obj.SetNamespace(req.Namespace)
// 		obj.SetName(strings.ReplaceAll(providerName, ".", "-"))
// 		// Set the provider's info
// 		// There can be some clever way to set the flavor size based on the application need
// 		// For simplicity, we use the default flavor introduced in the installation setup.
// 		// The default flavor is automatically set in the composition.
// 		// ProviderName includes providerName.ProviderRegion.ProviderZone.ProviderType
// 		spec := map[string]interface{}{
// 			"forProvider": map[string]interface{}{
// 				"vpcCidr": fmt.Sprintf("10.%d.3.0/24", idx),
// 				"gateway": map[string]string{
// 					// TODO: adjust the flavor more intelligently
// 					"flavor": "2vCPU-4GB",
// 				},
// 			},
// 			"providerRef": map[string]string{
// 				"providerName":   strings.Split(providerName, ".")[0],
// 				"providerRegion": provider.ProviderRegion,
// 				"providerZone":   provider.ProviderZone,
// 			},
// 		}
// 		obj.Object["spec"] = spec
// 		// if the provider is part of "SAVI", we need to add some labels representing
// 		// some external resources
// 		objLabels := make(map[string]string, 0)
// 		if strings.Contains(providerName, "os") {
// 			if provider.ProviderRegion == "scinet" || provider.ProviderRegion == "vaughan" {
// 				// get the gloabl CM
// 				globalCMList := &corev1.ConfigMapList{}
// 				if err := r.List(ctx, globalCMList, client.MatchingLabels{
// 					hv1a1.SKYCLUSTER_PROVIDERNAME_LABEL:   strings.Split(providerName, ".")[0],
// 					hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL: provider.ProviderRegion,
// 					hv1a1.SKYCLUSTER_PROVIDERTYPE_LABEL:   "global",
// 					hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL:   "global",
// 					hv1a1.SKYCLUSTER_CONFIGTYPE_LABEL:     hv1a1.SKYCLUSTER_ProvdiderMappings_LABEL,
// 				}); err != nil {
// 					return nil, errors.Wrap(err, "Error listing ConfigMaps.")
// 				}
// 				if len(globalCMList.Items) != 1 {
// 					return nil, errors.New("More than one ConfigMap found when generating manifests and this should not be happening.")
// 				}
// 				globalCM := globalCMList.Items[0]
// 				for k, v := range globalCM.Data {
// 					if strings.Contains(k, "ext-") {
// 						objLabels[fmt.Sprintf("%s/%s", hv1a1.SKYCLUSTER_API, k)] = v
// 					}
// 				}
// 				objLabels[hv1a1.SKYCLUSTER_MANAGEDBY_LABEL] = hv1a1.SKYCLUSTER_MANAGEDBY_VALUE
// 				objLabels[hv1a1.SKYCLUSTER_PROVIDERNAME_LABEL] = strings.Split(providerName, ".")[0]
// 				objLabels[hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL] = provider.ProviderRegion
// 				objLabels[hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL] = provider.ProviderZone
// 				// objLabels[corev1alpha1.SKYCLUSTER_PAUSE_LABEL] = "true"
// 				objLabels[hv1a1.SKYCLUSTER_ORIGINAL_NAME_LABEL] = providerName
// 			}
// 		}
// 		obj.SetLabels(objLabels)
// 		// We use original name with "." as the key and also
// 		// will use this as the SkyXRD.manifests.name value
// 		yamlObj, err := generateYAMLManifest(obj)
// 		if err != nil {
// 			return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 		}
// 		manifests[providerName] = hv1a1.SkyService{
// 			ComponentRef: corev1.ObjectReference{
// 				APIVersion: obj.GetAPIVersion(),
// 				Kind:       obj.GetKind(),
// 				Namespace:  obj.GetNamespace(),
// 				Name:       obj.GetName(),
// 			},
// 			Manifest: yamlObj,
// 			ProviderRef: hv1a1.ProviderRefSpec{
// 				// ProviderName should uniquely identify the provider
// 				// ProviderName = ProviderName.ProviderRegion.ProviderZone.ProviderType
// 				ProviderName:   providerName,
// 				ProviderRegion: provider.ProviderRegion,
// 				ProviderZone:   provider.ProviderZone,
// 			},
// 		}
// 	}
// 	return manifests, nil
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
// 	// Also, the SkyXRD object contains the original name
// 	xrdObj.SetName(component.ComponentRef.Name)

// 	// Get the corresponding Sky object to extract fields and set them in the XRD object
// 	skyObj := &unstructured.Unstructured{}
// 	skyObj.SetAPIVersion(component.ComponentRef.APIVersion)
// 	skyObj.SetKind(component.ComponentRef.Kind)
// 	if err := r.Get(ctx, client.ObjectKey{
// 		Namespace: req.Namespace,
// 		Name:      cmpntName,
// 	}, skyObj); err != nil {
// 		return nil, errors.Wrap(err, fmt.Sprintf("error getting object [%s].", component.ComponentRef.Name))
// 	}
// 	spec, err := GetNestedField(skyObj.Object, "spec")
// 	if err != nil {
// 		return nil, errors.Wrap(err, fmt.Sprintf("error getting nested field for object [%s].", component.ComponentRef.Name))
// 	}
// 	xrdObj.Object["spec"] = map[string]interface{}{
// 		"forProvider": spec,
// 		"providerRef": map[string]string{
// 			"providerName":   strings.Split(component.ProviderRef.ProviderName, ".")[0],
// 			"providerRegion": component.ProviderRef.ProviderRegion,
// 			"providerZone":   component.ProviderRef.ProviderZone,
// 		},
// 	}

// 	objLabels := skyObj.GetLabels()
// 	objLabels[hv1a1.SKYCLUSTER_MANAGEDBY_LABEL] = hv1a1.SKYCLUSTER_MANAGEDBY_VALUE
// 	objLabels[hv1a1.SKYCLUSTER_PROVIDERID_LABEL] = component.ProviderRef.ProviderName
// 	objLabels[hv1a1.SKYCLUSTER_PROVIDERNAME_LABEL] = strings.Split(component.ProviderRef.ProviderName, ".")[0]
// 	objLabels[hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL] = component.ProviderRef.ProviderRegion
// 	objLabels[hv1a1.SKYCLUSTER_PROVIDERREGIONALIAS_LABEL] = hv1a1.GetRegionAlias(component.ProviderRef.ProviderRegion)
// 	objLabels[hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL] = component.ProviderRef.ProviderZone
// 	objLabels[hv1a1.SKYCLUSTER_ORIGINAL_NAME_LABEL] = component.ComponentRef.Name
// 	// TODO: Remove the pause label before releasing
// 	// objLabels[corev1alpha1.SKYCLUSTER_PAUSE_LABEL] = "true"
// 	xrdObj.SetLabels(objLabels)

// 	yamlXrdObj, err := generateYAMLManifest(xrdObj)
// 	if err != nil {
// 		return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 	}
// 	// Set the return key name as name.kind to avoid conflicts
// 	return &hv1a1.SkyService{
// 		ComponentRef: corev1.ObjectReference{
// 			APIVersion: xrdObj.GetAPIVersion(),
// 			Kind:       xrdObj.GetKind(),
// 			Namespace:  xrdObj.GetNamespace(),
// 			// TODO: Is this name correct?
// 			Name: xrdObj.GetName(),
// 		},
// 		Manifest: yamlXrdObj,
// 		ProviderRef: hv1a1.ProviderRefSpec{
// 			ProviderName:   component.ProviderRef.ProviderName,
// 			ProviderRegion: component.ProviderRef.ProviderRegion,
// 			ProviderZone:   component.ProviderRef.ProviderZone,
// 		},
// 	}, nil
// }

// // generateSkyK8SCluster generates the SkyK8SCluster, SkyK8SCtrl, and SkyK8SAgent
// // it receives all deployments as one of the inputs
// func (r *SkyClusterReconciler) generateSkyK8SCluster(ctx context.Context, req ctrl.Request, components []hv1a1.SkyService) (*hv1a1.SkyService, error) {
// 	deploymentsPerProvider := make(map[string][]hv1a1.SkyService, 0)
// 	for _, deployment := range components {
// 		// ProviderName should uniquely identify the provider
// 		// ProviderName = ProviderName.ProviderRegion.ProviderZone.ProviderType
// 		providerName := deployment.ProviderRef.ProviderName
// 		deploymentsPerProvider[providerName] = append(deploymentsPerProvider[providerName], deployment)
// 	}

// 	// For deployments per provider, we derive pods requirements (cpu, memory), then
// 	// 1. Sort pods based on their (cpu and memory) requirements (decreasing)
// 	// 2. Sort the bins (flavors) available within the provider (cpu, memory) in decreasing order
// 	// 3. Assign each pod, the first existing bin that fits the pod requirements.
// 	// 4. If no bins exist, we create the smallest bin and assign the pod to it.
// 	// The cost of 4vCPU-8GB is exactly two times of 2vCPU-4GB, so for simplicity
// 	// we create a new bin of the smallest size and continue adding deployments to it
// 	// until we cannot assign anore more pods.
// 	selectedNodes := make(map[string][]computeResource, 0)
// 	for pName, deployments := range deploymentsPerProvider {
// 		// All of these deployments are for the same provider
// 		sortedDeployments := make([]computeResource, 0)
// 		for _, dep := range deployments {
// 			cr, err := r.calculateMinComputeResource(ctx, req, dep.ComponentRef.Name)
// 			if err != nil {
// 				return nil, errors.Wrap(err, fmt.Sprintf("Error getting minimum compute resource for deployment [%s].", dep.ComponentRef.Name))
// 			}
// 			sortedDeployments = append(sortedDeployments, *cr)
// 		}
// 		// Sort by CPU, then by RAM
// 		slices.SortFunc(sortedDeployments, sortComputeResources)

// 		// Get the provider's availalbe flavors
// 		pNameTrimmed := strings.Split(pName, ".")[0]
// 		// all deployments are for the same provider
// 		pRegion := deployments[0].ProviderRef.ProviderRegion
// 		pZone := deployments[0].ProviderRef.ProviderZone
// 		providerConfig, err := r.getProviderConfigMap(ctx, pNameTrimmed, pRegion, pZone)
// 		if err != nil {
// 			return nil, errors.Wrap(err,
// 				fmt.Sprintf("[Generate SkyK8S]\t Error getting configmap for provider [%s].", pName))
// 		}
// 		// Get flavors as computeResource struct
// 		pComputeResources, err := computeResourcesForFlavors(providerConfig.Data)
// 		if err != nil {
// 			return nil, errors.Wrap(err,
// 				fmt.Sprintf("[Generate SkyK8S]\t Error getting resources from flavors for provider [%s].", pName))
// 		}
// 		slices.SortFunc(pComputeResources, sortComputeResources)

// 		// Now we have sorted deployments and sorted provider's flavors
// 		// We can now proceed with the First-fit-decreasing bin packing algorithm
// 		nodes := make([]computeResource, 0)
// 		for _, dep := range sortedDeployments {
// 			ok, nodesPlaced := attemptPlaceDeployment(dep, nodes)
// 			if !ok {
// 				minComputeResource, err := r.calculateMinComputeResource(ctx, req, dep.name)
// 				if err != nil {
// 					return nil, errors.Wrap(err, fmt.Sprintf("Error getting minimum compute resource for deployment [%s].", dep.name))
// 				}
// 				// we get the minimum flavor that can accomodate the deployment
// 				// and add it to the nodes as a new bin
// 				newComResource, ok := findSuitableComputeResource(*minComputeResource, pComputeResources)
// 				if !ok {
// 					return nil, errors.New(fmt.Sprintf("could not finding suitable compute resource for deployment [%s].", dep.name))
// 				}
// 				nodes = append(nodes, *newComResource)
// 				slices.SortFunc(nodes, sortComputeResources)
// 			} else {
// 				nodes = nodesPlaced
// 			}
// 		}
// 		selectedNodes[pName] = nodes
// 	}

// 	// Having the nodes per provider, we can now generate SkyK8SCluster manifests
// 	// To create a SkyK8SCluster, we need to create a SkyK8SCluster only.
// 	xrdObj := &unstructured.Unstructured{}
// 	xrdObj.SetAPIVersion("xrds.skycluster.io/v1alpha1")
// 	xrdObj.SetKind("K8SCluster")
// 	xrdObj.SetNamespace(req.Namespace)
// 	xrdObj.SetName(req.Name)

// 	objLabels := make(map[string]string, 0)
// 	objLabels[hv1a1.SKYCLUSTER_MANAGEDBY_LABEL] = hv1a1.SKYCLUSTER_MANAGEDBY_VALUE
// 	// objLabels[corev1alpha1.SKYCLUSTER_PAUSE_LABEL] = "true"
// 	xrdObj.SetLabels(objLabels)

// 	// For controller, any of the provider can be used,
// 	// We prioritize selecting a "cloud" provider type over "nte" over "edge"
// 	ctrlProviders := map[string]*hv1a1.ProviderRefSpec{}
// 	for pName, _ := range selectedNodes {
// 		for _, skyCmpnt := range deploymentsPerProvider[pName] {
// 			if skyCmpnt.ProviderRef.ProviderType == "cloud" {
// 				ctrlProviders["cloud"] = &skyCmpnt.ProviderRef
// 				// We don't need to loop through all the deployments
// 				// as a "cloud" provider is found
// 				break
// 			}
// 			// Keep a record of the "nte" or "edge" provider
// 			// in case a "cloud" provider is not found
// 			ctrlProviders[skyCmpnt.ProviderRef.ProviderType] = &skyCmpnt.ProviderRef
// 			// We don't need to loop through all the deployments
// 			// All other components are of the same provider
// 			break
// 		}
// 		if _, ok := ctrlProviders["cloud"]; ok {
// 			// if a "cloud" provider is found, we don't need to loop through
// 			// all the providers
// 			break
// 		}
// 	}
// 	ctrlProvider := &hv1a1.ProviderRefSpec{}
// 	if _, ok := ctrlProviders["cloud"]; ok {
// 		ctrlProvider = ctrlProviders["cloud"]
// 	} else if _, ok := ctrlProviders["nte"]; ok {
// 		ctrlProvider = ctrlProviders["nte"]
// 	} else if _, ok := ctrlProviders["edge"]; ok {
// 		ctrlProvider = ctrlProviders["edge"]
// 	} else {
// 		return nil, errors.New("Error, no provider for SkyK8S controller found.")
// 	}

// 	// For agents we need to create a SkyK8SAgent for each provider
// 	agentSpecs := make([]map[string]any, 0)
// 	for pName, nodes := range selectedNodes {
// 		agentProvider := deploymentsPerProvider[pName][0].ProviderRef
// 		for idx, node := range nodes {
// 			agentSpec := map[string]any{
// 				"image":  "ubuntu-22.04",
// 				"flavor": node.name,
// 				"name":   fmt.Sprintf("agent-%d-%s-%s", idx+1, agentProvider.ProviderRegion, agentProvider.ProviderZone),
// 				"providerRef": map[string]string{
// 					"providerName":   strings.Split(agentProvider.ProviderName, ".")[0],
// 					"providerRegion": agentProvider.ProviderRegion,
// 					"providerZone":   agentProvider.ProviderZone,
// 					"providerType":   agentProvider.ProviderType,
// 				},
// 			}
// 			agentSpecs = append(agentSpecs, agentSpec)
// 		}
// 	}

// 	// Controller and Agents are created
// 	// TODO: Ctrl flavor should be set based on the application need
// 	xrdObj.Object["spec"] = map[string]any{
// 		"forProvider": map[string]any{
// 			"privateRegistry": "registry.skycluster.io",
// 			"ctrl": map[string]any{
// 				"image":  "ubuntu-22.04",
// 				"flavor": "2vCPU-4GB",
// 				"providerRef": map[string]string{
// 					"providerName":   strings.Split(ctrlProvider.ProviderName, ".")[0],
// 					"providerRegion": ctrlProvider.ProviderRegion,
// 					"providerZone":   ctrlProvider.ProviderZone,
// 				},
// 			},
// 			"agents": agentSpecs,
// 		},
// 	}

// 	yamlObj, err := generateYAMLManifest(xrdObj)
// 	if err != nil {
// 		return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 	}
// 	// Set the return key name as name.kind to avoid conflicts
// 	return &hv1a1.SkyService{
// 		ComponentRef: corev1.ObjectReference{
// 			APIVersion: xrdObj.GetAPIVersion(),
// 			Kind:       xrdObj.GetKind(),
// 			Namespace:  xrdObj.GetNamespace(),
// 			Name:       xrdObj.GetName(),
// 		},
// 		Manifest: yamlObj,
// 		// We set providerRef as the controller's provider
// 		ProviderRef: hv1a1.ProviderRefSpec{
// 			ProviderName:   ctrlProvider.ProviderName,
// 			ProviderRegion: ctrlProvider.ProviderRegion,
// 			ProviderZone:   ctrlProvider.ProviderZone,
// 		},
// 	}, nil
// }

// generateSkyAppManifests generates the deployments and services manifests for the application
// for the remote cluster
// func (r *SkyClusterReconciler) generateSkyAppManifests(ctx context.Context, req ctrl.Request, deployMap hv1a1.SkyService) ([]hv1a1.SkyService, error) {
// 	manifests := make([]hv1a1.SkyService, 0)
// 	deploymentList := make([]appsv1.Deployment, 0)
// 	for _, deployItem := range deployMap.Component {
// 		// based on the type of services we may modify the objects' spec
// 		switch strings.ToLower(deployItem.ComponentRef.Kind) {
// 		case "deployment":
// 			// The deployments should have a node selector field that
// 			// restricts the deployment to a specific provider's node
// 			// Also the deployment should be wrapped in "Object" which
// 			// the SkyApp controller can take care of it (for now).
// 			deploy := &appsv1.Deployment{}
// 			if err := r.Get(ctx, client.ObjectKey{
// 				Namespace: req.Namespace, Name: deployItem.ComponentRef.Name,
// 			}, deploy); err != nil {
// 				return nil, errors.Wrap(err, "Error getting Deployment.")
// 			}

// 			// We need to create a replicated deployment with node selector and labels
// 			// Deep copy the deploy into a new object, we manually copy fields as
// 			// the deployment object itself contains a lot of fields that we don't need
// 			newDeploy := generateNewDeplyFromDeploy(deploy)

// 			// We need to add the node selector to the deployment
// 			// deployItem.Provider.ProviderName includes providerName.ProviderRegion.ProviderZone.ProviderType
// 			// We need to extract the first three parts and replace "." with "-"
// 			pIdLabel := hv1a1.SKYCLUSTER_PROVIDERID_LABEL
// 			providerId := strings.Join(strings.Split(deployItem.ProviderRef.ProviderName, ".")[:3], "-")

// 			mngByLabel := hv1a1.SKYCLUSTER_MANAGEDBY_LABEL
// 			mngByLabelValue := hv1a1.SKYCLUSTER_MANAGEDBY_VALUE

// 			pNameLabel := hv1a1.SKYCLUSTER_PROVIDERNAME_LABEL
// 			regLabel := hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL
// 			regLabelAlias := hv1a1.SKYCLUSTER_PROVIDERREGIONALIAS_LABEL
// 			zoneLabel := hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL

// 			// spec.template.spec.nodeSelector
// 			newDeploy.Spec.Template.Spec.NodeSelector = map[string]string{
// 				hv1a1.SKYCLUSTER_PROVIDERID_LABEL: providerId,
// 			}

// 			// spec.template.metadata.labels
// 			if newDeploy.Spec.Template.ObjectMeta.Labels == nil {
// 				newDeploy.Spec.Template.ObjectMeta.Labels = make(map[string]string)
// 			}
// 			newDeploy.Spec.Template.ObjectMeta.Labels[pIdLabel] = providerId
// 			newDeploy.Spec.Template.ObjectMeta.Labels[pNameLabel] = strings.Split(deployItem.ProviderRef.ProviderName, ".")[0]
// 			newDeploy.Spec.Template.ObjectMeta.Labels[regLabel] = deployItem.ProviderRef.ProviderRegion
// 			newDeploy.Spec.Template.ObjectMeta.Labels[regLabelAlias] = hv1a1.GetRegionAlias(deployItem.ProviderRef.ProviderRegion)
// 			newDeploy.Spec.Template.ObjectMeta.Labels[zoneLabel] = deployItem.ProviderRef.ProviderZone

// 			// spec.selector
// 			if newDeploy.Spec.Selector == nil {
// 				newDeploy.Spec.Selector = &metav1.LabelSelector{}
// 			}
// 			if newDeploy.Spec.Selector.MatchLabels == nil {
// 				newDeploy.Spec.Selector.MatchLabels = make(map[string]string)
// 			}
// 			newDeploy.Spec.Selector.MatchLabels[pIdLabel] = providerId

// 			// Update name to include providerId
// 			newDeploy.Name = fmt.Sprintf("%s-%s", deployItem.ComponentRef.Name, deployItem.ProviderRef.ProviderName)

// 			// Add general labels to the deployment
// 			if newDeploy.ObjectMeta.Labels == nil {
// 				newDeploy.ObjectMeta.Labels = make(map[string]string)
// 			}
// 			newDeploy.ObjectMeta.Labels[mngByLabel] = mngByLabelValue
// 			newDeploy.ObjectMeta.Labels[pIdLabel] = providerId
// 			newDeploy.ObjectMeta.Labels[pNameLabel] = strings.Split(deployItem.ProviderRef.ProviderName, ".")[0]
// 			newDeploy.ObjectMeta.Labels[regLabel] = deployItem.ProviderRef.ProviderRegion
// 			newDeploy.ObjectMeta.Labels[regLabelAlias] = hv1a1.GetRegionAlias(deployItem.ProviderRef.ProviderRegion)
// 			newDeploy.ObjectMeta.Labels[zoneLabel] = deployItem.ProviderRef.ProviderZone

// 			// We need to add the deployment to the manifests
// 			yamlObj, err := generateYAMLManifest(newDeploy)
// 			if err != nil {
// 				return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 			}
// 			manifests = append(manifests, hv1a1.SkyService{
// 				ComponentRef: corev1.ObjectReference{
// 					APIVersion: newDeploy.APIVersion,
// 					Kind:       newDeploy.Kind,
// 					Namespace:  newDeploy.Namespace,
// 					Name:       newDeploy.Name,
// 				},
// 				Manifest: yamlObj,
// 				ProviderRef: hv1a1.ProviderRefSpec{
// 					ProviderName:   deployItem.ProviderRef.ProviderName,
// 					ProviderRegion: deployItem.ProviderRef.ProviderRegion,
// 					ProviderZone:   deployItem.ProviderRef.ProviderZone,
// 				},
// 			})
// 			deploymentList = append(deploymentList, newDeploy)
// 		}
// 	}

// 	// There could be services submitted as part of the application manifests,
// 	// The services should use managed-by label to be identified
// 	// and we should match the app selector with the deployment's app labels
// 	// Once we identify the services, we create correspodning services
// 	// and istio configuration for remote cluster
// 	svcList := &corev1.ServiceList{}
// 	if err := r.List(ctx, svcList, client.MatchingLabels{
// 		hv1a1.SKYCLUSTER_MANAGEDBY_LABEL: hv1a1.SKYCLUSTER_MANAGEDBY_VALUE,
// 	}); err != nil {
// 		return nil, errors.Wrap(err, "Error listing Services.")
// 	}
// 	for _, svc := range svcList.Items {
// 		// Check if the svc is referring to one of the deployments in the deployment list
// 		// We need to have one copy of the service by default, as internal service
// 		// For each provider, we need to create a new service with the same provider selector
// 		// to control traffic distribution using istio
// 		oneSvc := generateNewServiceFromService(&svc)
// 		if oneSvc.GetLabels() == nil {
// 			oneSvc.SetLabels(make(map[string]string))
// 		}
// 		oneSvc.GetLabels()[hv1a1.SKYCLUSTER_SVCTYPE_LABEL] = "app-face"

// 		yamlObj, err := generateYAMLManifest(oneSvc)
// 		if err != nil {
// 			return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 		}
// 		manifests = append(manifests, hv1a1.SkyService{
// 			ComponentRef: corev1.ObjectReference{
// 				APIVersion: oneSvc.APIVersion,
// 				Kind:       oneSvc.Kind,
// 				Namespace:  oneSvc.Namespace,
// 				Name:       oneSvc.Name,
// 			},
// 			Manifest: yamlObj,
// 		})
// 		for _, deploy := range deploymentList {
// 			if !deploymentHasLabels(&deploy, svc.Spec.Selector) {
// 				continue
// 			}
// 			// We need to create a new service with the same selector
// 			// and add the provider's node selector to the service
// 			newSvc := generateNewServiceFromService(&svc)
// 			providerId := deploy.Labels[hv1a1.SKYCLUSTER_PROVIDERID_LABEL]
// 			newSvc.Spec.Selector = deploy.Spec.Selector.MatchLabels
// 			newSvc.ObjectMeta.Name = fmt.Sprintf("%s-%s", svc.Name, providerId)

// 			pIdLabel := hv1a1.SKYCLUSTER_PROVIDERID_LABEL
// 			regLabel := hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL
// 			regLabelAlias := hv1a1.SKYCLUSTER_PROVIDERREGIONALIAS_LABEL
// 			zoneLabel := hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL

// 			newSvc.ObjectMeta.Labels[pIdLabel] = providerId
// 			newSvc.ObjectMeta.Labels[regLabelAlias] = hv1a1.GetRegionAlias(deploy.Labels[pIdLabel])
// 			newSvc.ObjectMeta.Labels[regLabel] = deploy.Labels[regLabel]
// 			newSvc.ObjectMeta.Labels[zoneLabel] = deploy.Labels[zoneLabel]

// 			yamlObj, err := generateYAMLManifest(newSvc)
// 			if err != nil {
// 				return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 			}
// 			manifests = append(manifests, hv1a1.SkyService{
// 				ComponentRef: corev1.ObjectReference{
// 					APIVersion: newSvc.APIVersion,
// 					Kind:       newSvc.Kind,
// 					Namespace:  newSvc.Namespace,
// 					Name:       newSvc.Name,
// 				},
// 				Manifest: yamlObj,
// 			})
// 			// We break here as I expect only one service per deployment
// 			break
// 		}
// 	}

// 	return manifests, nil
// }

// func (r *SkyClusterReconciler) createXRDs(ctx context.Context, req ctrl.Request, deployMap hv1a1.SkyService) ([]hv1a1.SkyService, []hv1a1.SkyService, error) {
// 	manifests := make([]hv1a1.SkyService, 0)
// 	// skyObjs := map[string]unstructured.Unstructured{}
// 	// ######### Providers
// 	// Each deployment comes with component info (e.g. kind and apiVersion and name)
// 	// as well as the provider info (e.g. name, region, zone, type) that it should be deployed on
// 	// We extract all provider's info and create corresponding SkyProvider objects for each provider
// 	providersManifests, err := r.generateProviderManifests(ctx, req, deployMap.Component)
// 	if err != nil {
// 		return nil, nil, errors.Wrap(err, "Error generating provider manifests.")
// 	}
// 	for _, obj := range providersManifests {
// 		manifests = append(manifests, obj)
// 		// skyObjs[skyObjName] = obj
// 	}

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
