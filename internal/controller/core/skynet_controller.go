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
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

// SkyNetReconciler reconciles a SkyNet object
type SkyNetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=skynets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skynets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skynets/finalizers,verbs=update

func (r *SkyNetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started")

	// Fetch the object
	var skynet cv1a1.SkyNet
	if err := r.Get(ctx, req.NamespacedName, &skynet); err != nil {
		r.Logger.Info("SkyNet resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Must generate application manifests
	// (i.e. deployments, services, istio configurations, etc.) and
	// submit them to the remote cluster using Kubernetes Provider (object)
	// We create manifest and submit it to the SkyAPP controller for further processing
	
	// appManifests, err := r.generateSkyAppManifests(deployMap)
	// if err != nil {
	// 	return nil, nil, errors.Wrap(err, "Error generating SkyApp manifests.")
	// }
	manifests, err := r.generateAppManifests(skynet.Namespace, skynet.Spec.DeployMap.Component)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate application manifests") }

	skynet.Status.Manifests = manifests
	
	// err := r.generateIstioCfg()
	// if err != nil {
	// 	_ = r.updateStatus(&skynet) // best effort update
	// 	return ctrl.Result{}, errors.Wrap(err, "failed to generate Istio configuration")
	// }

	if err := r.updateStatus(&skynet); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to update SkyNet status")
	}
	return ctrl.Result{}, nil
}

func (r *SkyNetReconciler) updateStatus(skynet *cv1a1.SkyNet) error {
	if err := r.Status().Update(context.TODO(), skynet); err != nil {
		return errors.Wrap(err, "failed to update SkyNet status with application manifests")
	}
	return nil
}

// generateAppManifests generates application manifests based on the deploy plan
// for distributed environment, including replicated deployments and services
func (r *SkyNetReconciler) generateAppManifests(ns string, cmpnts []hv1a1.SkyService) ([]hv1a1.SkyService, error) {
	manifests := make([]hv1a1.SkyService, 0)
	deploymentList := make([]appsv1.Deployment, 0)
	for _, deployItem := range cmpnts {
		// based on the type of services we may modify the objects' spec
		switch strings.ToLower(deployItem.ComponentRef.Kind) {
		case "deployment":
			// Also the deployment should be wrapped in "Object" which
			// the SkyApp controller can take care of it (for now).
			deploy := &appsv1.Deployment{}
			if err := r.Get(context.TODO(), client.ObjectKey{
				Namespace: ns, Name: deployItem.ComponentRef.Name,
			}, deploy); err != nil {return nil, errors.Wrap(err, "Error getting Deployment.")}

			// Create a replicated deployment with node selector, labels, annotations
			// Discard all other fields that are not necessary
			newDeploy := generateNewDeplyFromDeploy(deploy)

			podLabels := newDeploy.Spec.Template.ObjectMeta.Labels
			podLabels["skycluster.io/managed-by"] = "skycluster"
			podLabels["skycluster.io/provider-name"] = deployItem.ProviderRef.Name
			podLabels["skycluster.io/provider-region"] = deployItem.ProviderRef.Region
			podLabels["skycluster.io/provider-zone"] = deployItem.ProviderRef.Zone
			podLabels["skycluster.io/provider-platform"] = deployItem.ProviderRef.Platform
			podLabels["skycluster.io/provider-region-alias"] = hv1a1.GetRegionAlias(deployItem.ProviderRef.RegionAlias)
			podLabels["skycluster.io/provider-type"] = deployItem.ProviderRef.Type

			// spec.selector
			if newDeploy.Spec.Selector == nil {newDeploy.Spec.Selector = &metav1.LabelSelector{}}
			if newDeploy.Spec.Selector.MatchLabels == nil {newDeploy.Spec.Selector.MatchLabels = make(map[string]string)}
			// newDeploy.Spec.Selector.MatchLabels[pIdLabel] = providerId

			// Add general labels to the deployment
			deployLabels := newDeploy.ObjectMeta.Labels
			deployLabels["skycluster.io/managed-by"] = "skycluster"
			deployLabels["skycluster.io/provider-name"] = deployItem.ProviderRef.Name
			deployLabels["skycluster.io/provider-region"] = deployItem.ProviderRef.Region
			deployLabels["skycluster.io/provider-zone"] = deployItem.ProviderRef.Zone
			deployLabels["skycluster.io/provider-platform"] = deployItem.ProviderRef.Platform
			deployLabels["skycluster.io/provider-region-alias"] = hv1a1.GetRegionAlias(deployItem.ProviderRef.RegionAlias)
			deployLabels["skycluster.io/provider-type"] = deployItem.ProviderRef.Type
			
			// We need to add the deployment to the manifests
			yamlObj, err := generateYAMLManifest(newDeploy)
			if err != nil {
				return nil, errors.Wrap(err, "Error generating YAML manifest.")
			}
			manifests = append(manifests, hv1a1.SkyService{
				ComponentRef: corev1.ObjectReference{
					APIVersion: newDeploy.APIVersion,
					Kind:       newDeploy.Kind,
					Namespace:  newDeploy.Namespace,
					Name:       newDeploy.Name,
				},
				Manifest: yamlObj,
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:   deployItem.ProviderRef.Name,
					Region: deployItem.ProviderRef.Region,
					Zone:   deployItem.ProviderRef.Zone,
				},
			})
			deploymentList = append(deploymentList, newDeploy)
		}
	}

	// There could be services submitted as part of the application manifests,
	// Since services are not specified in deployPlan, 
	// they must be tagged with managed-by label to be identified
	// We must match the app selector with the deployment's app labels
	// Once identified, we create corresponding services
	// and istio configuration for remote cluster
	svcList := &corev1.ServiceList{}
	if err := r.List(context.TODO(), svcList, client.MatchingLabels{
		"skycluster.io/managed-by": "skycluster",
	}); err != nil {return nil, errors.Wrap(err, "error listing Services.")}
	
	for _, svc := range svcList.Items {
		// Check if the svc is referring to one of the deployments in the deployment list
		// For each provider, we need to create a new service with the same provider selector
		// to control traffic distribution using istio
		thisSvc := generateNewServiceFromService(&svc)
		
		svcLabels := thisSvc.ObjectMeta.Labels
		svcLabels["skycluster.io/service-type"] = "app-face"

		yamlObj, err := generateYAMLManifest(thisSvc)
		if err != nil {return nil, errors.Wrap(err, "Error generating YAML manifest.")}

		manifests = append(manifests, hv1a1.SkyService{
			ComponentRef: corev1.ObjectReference{
				APIVersion: thisSvc.APIVersion,
				Kind:       thisSvc.Kind,
				Namespace:  thisSvc.Namespace,
				Name:       thisSvc.Name,
			},
			Manifest: yamlObj,
		})

		// prepare replicated services for each deployment in each remote cluster
		for _, deploy := range deploymentList {
			if !deploymentHasLabels(&deploy, svc.Spec.Selector) { continue }
			
			// We need to create a new service with the same selector
			// and add the provider's node selector to the service
			newSvc := generateNewServiceFromService(&svc)
			
			// All service is identical but with different labels
			labels := newSvc.ObjectMeta.Labels
			labels["skycluster.io/managed-by"] = "skycluster"
			labels["skycluster.io/provider-name"] = deploy.Labels["skycluster.io/provider-name"]
			labels["skycluster.io/provider-region"] = deploy.Labels["skycluster.io/provider-region"]
			labels["skycluster.io/provider-zone"] = deploy.Labels["skycluster.io/provider-zone"]
			labels["skycluster.io/provider-platform"] = deploy.Labels["skycluster.io/provider-platform"]
			labels["skycluster.io/provider-region-alias"] = deploy.Labels["skycluster.io/provider-region-alias"]
			labels["skycluster.io/provider-type"] = deploy.Labels["skycluster.io/provider-type"]
			
			yamlObj, err := generateYAMLManifest(newSvc)
			if err != nil {return nil, errors.Wrap(err, "Error generating YAML manifest.")}
			
			manifests = append(manifests, hv1a1.SkyService{
				ComponentRef: corev1.ObjectReference{
					APIVersion: newSvc.APIVersion,
					Kind:       newSvc.Kind,
					Namespace:  newSvc.Namespace,
					Name:       newSvc.Name,
				},
				Manifest: yamlObj,
			})
		}
	}

	return manifests, nil
}


// SetupWithManager sets up the controller with the Manager.
func (r *SkyNetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.SkyNet{}).
		Named("core-skynet").
		Complete(r)
}
