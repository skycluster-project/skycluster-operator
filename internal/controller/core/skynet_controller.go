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
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/pkg/errors"
	"github.com/samber/lo"

	istiov1 "istio.io/api/networking/v1"
	istioClient "istio.io/client-go/pkg/apis/networking/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller"
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

	provCfgNameMap, err := r.getProviderConfigNameMap(skynet)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to get provider config name map")
	}
	r.Logger.Info("Fetched provider config name map.", "count", len(provCfgNameMap))

	appId, ok := skynet.Labels["skycluster.io/app-id"]
	if !ok || appId == "" {
		return ctrl.Result{}, errors.New("missing required label: skycluster.io/app-id")
	}

	manifests := make([]*hv1a1.SkyObject, 0)

	// Must generate application manifests
	// (i.e. deployments, services, istio configurations, etc.) and
	// submit them to the remote cluster using Kubernetes Provider (object)
	// Manifest are submitted through "Object" CRD from Crossplane

	nsManifests, err := r.generateNamespaceManifests(skynet.Namespace, appId, skynet.Spec.DeployMap.Component, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate namespace manifests") }
	manifests = append(manifests, nsManifests...)

	cdManifests, err := r.generateConfigDataManifests(skynet.Namespace, appId, skynet.Spec.DeployMap.Component, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate config data manifests") }
	manifests = append(manifests, cdManifests...)

	depManifests, err := r.generateDeployManifests(skynet.Namespace, skynet.Spec.DeployMap, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate application manifests") }
	manifests = append(manifests, depManifests...)

	svcManifests, err := r.generateServiceManifests(skynet.Namespace, appId, skynet.Spec.DeployMap.Component, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate service manifests") }
	manifests = append(manifests, svcManifests...)

	manifestsIstio, err := r.generateIstioConfig(skynet.Namespace, appId, skynet.Spec.DeployMap.Edges, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate istio configuration manifests") }
	manifests = append(manifests, manifestsIstio...)

	if skynet.Status.Objects == nil { skynet.Status.Objects = make([]hv1a1.SkyObject, 0) }
	if len(skynet.Status.Objects) != len(manifests) || !reflect.DeepEqual(skynet.Status.Objects, manifests) {
    skynet.Status.Objects = make([]hv1a1.SkyObject, 0)
		for _, m := range manifests {
			skynet.Status.Objects = append(skynet.Status.Objects, *m)
		}
		r.Logger.Info("SkyNet status manifests differ from generated manifests, updating status.")

    if err := r.Status().Update(ctx, &skynet); err != nil {
        return ctrl.Result{}, errors.Wrap(err, "error updating SkyNet status")
    }
		r.Logger.Info("Updated SkyNet status manifests.", "count", len(manifests))
	}

	// TODO: must check for deleted manifests and delete them from the remote clusters
	// For now, we only support adding new manifests
	if skynet.Spec.Approve {
		r.Logger.Info("Approval given, creating manifest objects in the cluster.")
		for _, m := range manifests {
			obj, err := generateUnstructuredWrapper(m, &skynet, r.Scheme)
			if err != nil {return ctrl.Result{}, errors.Wrap(err, "failed to generate unstructured wrapper")}
			
			if err := r.Create(ctx, obj); err != nil {
				if apierrors.IsAlreadyExists(err) {
					current := &unstructured.Unstructured{}
					current.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "skycluster.io",
						Version: "v1alpha1",
						Kind:    "Object",
					})
					if err := r.Get(ctx, types.NamespacedName{
						Name:      obj.GetName(),
						Namespace: obj.GetNamespace(),
					}, current); err != nil { 
						return ctrl.Result{}, errors.Wrap(err, "error getting existing manifest object") 
					}

					obj.SetResourceVersion(current.GetResourceVersion())
					if err := r.Update(ctx, obj); err != nil { return ctrl.Result{}, errors.Wrap(err, "error updating manifest object") }
				} else { return ctrl.Result{}, errors.Wrap(err, "error creating manifest object") }
			}
		}
	}
	return ctrl.Result{}, nil
}

func generateUnstructuredWrapper(skyObj *hv1a1.SkyObject, owner *cv1a1.SkyNet, scheme *runtime.Scheme) (*unstructured.Unstructured, error) {
	
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("skycluster.io/v1alpha1")
	obj.SetKind("Object")
	obj.SetName(skyObj.Name)
	obj.SetNamespace(owner.Namespace)
	manifestMap, err := skyObj.ManifestAsMap()
	if err != nil { return nil, errors.Wrap(err, "failed to convert manifest to map") }
	obj.Object["spec"] = map[string]any{
		"manifest":     manifestMap,
		"compositeDeletePolicy": "Foreground",
		"providerConfigRef": map[string]any{
			"name":      skyObj.ProviderRef.ConfigName,
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
    Group:   "skycluster.io",
    Version: "v1alpha1",
    Kind:    "Object",
	})
	return obj, nil
}

// This is the name of corresponding XKube "Object" provider config (the remote k8s cluster)
// it returns a map of provider profile name to provider config name
// the local cluster is mapped to "local" key
func (r *SkyNetReconciler) getProviderConfigNameMap(skynet cv1a1.SkyNet) (map[string]string, error) {
	provProfiles, err := r.fetchProviderProfilesMap()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch provider profiles")
	}

	// the name of corresponding xprovider object is the same as provider profile name
	cfgPerProv := map[string]string{}

	cfgPerProvList := &unstructured.UnstructuredList{}
	cfgPerProvList.SetGroupVersionKind(
		schema.GroupVersionKind{
			Group:   "skycluster.io",
			Version: "v1alpha1",
			Kind:    "XKube",
		},
	)
	if err := r.List(context.TODO(), cfgPerProvList); err != nil {
		return nil, errors.Wrap(err, "failed to list XProvider objects")
	}

	for pName, pp := range provProfiles { // for each enabled provider profile
		for _, xProvObj := range cfgPerProvList.Items {
			xpRegion, err1 := utils.GetNestedString(xProvObj.Object, "spec", "providerRef", "region")
			xpPlatform, err2 := utils.GetNestedString(xProvObj.Object, "spec", "providerRef", "platform")
			if err1 != nil || err2 != nil {
				continue
			}
			if xpPlatform != pp.Spec.Platform || xpRegion != pp.Spec.Region {
				continue
			}
			// platform and region match (TODO: consider zones later)
			provCfgName, err := utils.GetNestedString(xProvObj.Object, "status", "providerConfigs", "k8s")
			if err != nil {
				continue
			}
			cfgPerProv[pName] = provCfgName
		}
	}

	// in addition to the remote cluster, we need provider config for the local cluster
	localSetup := &unstructured.UnstructuredList{}
	localSetup.SetGroupVersionKind(
		schema.GroupVersionKind{
			Group:   "skycluster.io",
			Version: "v1alpha1",
			Kind:    "XSetup",
		},
	)
	if err := r.List(context.TODO(), localSetup); err != nil {
		return nil, errors.Wrap(err, "failed to list XSetup objects")
	}

	// normally there should be only one XSetup object
	if len(localSetup.Items) == 0 {
		return nil, errors.New("no XSetup object found for local cluster")
	}
	if len(localSetup.Items) > 1 {
		return nil, errors.New("multiple XSetup objects found for local cluster")
	}

	localXSetup := localSetup.Items[0]
	localProvCfgName, err := utils.GetNestedString(localXSetup.Object, "status", "providerConfig", "kubernetes", "name")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get local provider config name from XSetup")
	}

	cfgPerProv["local"] = localProvCfgName
	return cfgPerProv, nil
}

// get the deployments' namespaces and generate namespace manifests
// namespaces must be generated for all clusters
func (r *SkyNetReconciler) generateConfigDataManifests(ns string, appId string, cmpnts []hv1a1.SkyService, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {

	manifests := make([]*hv1a1.SkyObject, 0)
	selectedProvNames := make([]string, 0)

	// Since we don't know and do not care about the location of configmaps,
	// we have them distributed to all clusters in the distributed setup
	for _, deployItem := range cmpnts {
		// based on the type of services we may modify the objects' spec
		switch strings.ToLower(deployItem.ComponentRef.Kind) {
		case "configmap":
			deploy := &appsv1.Deployment{}
			if err := r.Get(context.TODO(), client.ObjectKey{
				Namespace: ns, Name: deployItem.ComponentRef.Name,
			}, deploy); err != nil {return nil, errors.Wrap(err, "error getting Deployment.")}

			// collect all providers used in the deployment plan
			selectedProvNames = append(selectedProvNames, deployItem.ProviderRef.Name)
		}
	}
	selectedProvNames = lo.Uniq(selectedProvNames)

	// now for each configmap, we create a configmap manifest
	// along with its labels
	cmList := &corev1.ConfigMapList{}
	if err := r.List(context.TODO(), cmList, client.MatchingLabels{
		"skycluster.io/app-scope": "distributed",
		"skycluster.io/app-id":    appId,
	}); err != nil {
		return nil, errors.Wrap(err, "error listing configmaps.")
	}

	for _, cmObj := range cmList.Items {
		// selectedProvNames contains the list of provider names selected for deployments
		// a configmap manifest must be created for each provider
		for _, pName := range selectedProvNames {
			// prepare namespace object
			newNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: cmObj.Name,
					Labels: cmObj.Labels,
				},
			}
			objMap, err := objToMap(newNs)
			if err != nil {return nil, errors.Wrap(err, "error converting ConfigMap to map.")}

			objMap["kind"] = "ConfigMap"
			objMap["apiVersion"] = "v1"
	
			name := "cm-" + newNs.Name + "-" + pName
			rand := RandSuffix(name)
			name = name[0:int(math.Min(float64(len(name)), 20))]
			name = name + "-" + rand

			obj, err := generateObjectWrapper(name, "", objMap, provCfgNameMap[pName])
			if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }
			manifests = append(manifests, obj)
		}
	}

	secrets := &corev1.SecretList{}
	if err := r.List(context.TODO(), secrets, client.MatchingLabels{
		"skycluster.io/app-scope": "distributed",
		"skycluster.io/app-id":    appId,
	}); err != nil {
		return nil, errors.Wrap(err, "error listing secrets.")
	}

	for _, secObj := range secrets.Items {
		// selectedProvNames contains the list of provider names selected for deployments
		// a secret manifest must be created for each provider
		for _, pName := range selectedProvNames {
			// prepare namespace object
			newNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: secObj.Name,
					Labels: secObj.Labels,
				},
			}
			objMap, err := objToMap(newNs)
			if err != nil {return nil, errors.Wrap(err, "error converting Secret to map.")}

			objMap["kind"] = "Secret"
			objMap["apiVersion"] = "v1"
	
			name := "sec-" + newNs.Name + "-" + pName
			rand := RandSuffix(name)
			name = name[0:int(math.Min(float64(len(name)), 20))]
			name = name + "-" + rand

			obj, err := generateObjectWrapper(name, "", objMap, provCfgNameMap[pName])
			if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }
			manifests = append(manifests, obj)
		}
	}

	return manifests, nil
}

func (r *SkyNetReconciler) generateNamespaceManifests(ns string, appId string, cmpnts []hv1a1.SkyService, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
	// manifests := make([]hv1a1.SkyService, 0)
	manifests := make([]*hv1a1.SkyObject, 0)
	selectedNs := make([]string, 0)
	selectedProvNames := make([]string, 0)

	// Deployments are created in selected remote clusters. We use "Object" CRD
	// to wrap the deployment object along with provider reference
	for _, deployItem := range cmpnts {
		// based on the type of services we may modify the objects' spec
		switch strings.ToLower(deployItem.ComponentRef.Kind) {
		case "deployment":
			deploy := &appsv1.Deployment{}
			if err := r.Get(context.TODO(), client.ObjectKey{
				Namespace: ns, Name: deployItem.ComponentRef.Name,
			}, deploy); err != nil {return nil, errors.Wrap(err, "error getting Deployment.")}

			// collect all providers used in the deployment plan
			selectedProvNames = append(selectedProvNames, deployItem.ProviderRef.Name)

			if deploy.Namespace != "" { // capture namespace from the deployment if specified
				selectedNs = append(selectedNs, deploy.Namespace)
			} else {
				selectedNs = append(selectedNs, ns) // default
			}
		}
	}
	selectedNs = lo.Uniq(selectedNs)
	selectedProvNames = lo.Uniq(selectedProvNames)
	// selectedProvNames = append(selectedProvNames, "local") // Do we need this?

	// now for each namespace, we create a namespace manifest
	// along with its labels
	nsList := &corev1.NamespaceList{}
	if err := r.List(context.TODO(), nsList, client.MatchingLabels{
		"skycluster.io/app-scope": "distributed",
		"skycluster.io/app-id":    appId,
	}); err != nil {
		return nil, errors.Wrap(err, "error listing namespaces.")
	}

	for _, nsObj := range nsList.Items {
		if !slices.Contains(selectedNs, nsObj.Name) {
			if v, ok := nsObj.Labels["skycluster.io/app-scope"]; !ok || v != "distributed" {
				continue // The namespace does not belong to app deployments
			}
		}

		labels := nsObj.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		labels["skycluster.io/managed-by"] = "skycluster"
		labels["istio-injection"] = "enabled"

		// selectedProvNames contains the list of provider names selected for deployments
		// a namespace manifest must be created for each provider
		for _, pName := range selectedProvNames {
			// prepare namespace object
			newNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsObj.Name,
					Labels: labels,
				},
			}
			objMap, err := objToMap(newNs)
			if err != nil {return nil, errors.Wrap(err, "error converting Namespace to map.")}
	
			objMap["kind"] = "Namespace"
			objMap["apiVersion"] = "v1"
	
			name := "ns-" + newNs.Name + "-" + pName
			rand := RandSuffix(name)
			name = name[0:int(math.Min(float64(len(name)), 20))]
			name = name + "-" + rand

			obj, err := generateObjectWrapper(name, "", objMap, provCfgNameMap[pName])
			if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }
			manifests = append(manifests, obj)
		}
	}
	return manifests, nil
}

func (r *SkyNetReconciler) generateServiceManifests(ns string, appId string, cmpnts []hv1a1.SkyService, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
	// manifests := make([]hv1a1.SkyService, 0)
	manifests := make([]*hv1a1.SkyObject, 0)
	selectedProvNames := make([]string, 0)

	for _, deployItem := range cmpnts {
		switch strings.ToLower(deployItem.ComponentRef.Kind) {
		case "deployment": // we collect provider names from deployments
			pName := deployItem.ProviderRef.Name
			// collect all providers used in the deployment plan
			selectedProvNames = append(selectedProvNames, pName)
		}
	}

	selectedProvNames = lo.Uniq(selectedProvNames)
	// TODO: Do we need to create services in the local cluster?
	// selectedProvNames = append(selectedProvNames, "local")

	selectedServices := &corev1.ServiceList{}
	if err := r.List(context.TODO(), selectedServices, client.MatchingLabels{
		"skycluster.io/app-scope": "distributed",
		"skycluster.io/app-id":    appId,
	}); err != nil {return nil, errors.Wrap(err, "error listing Services.")}

	for _, svc := range selectedServices.Items {
		
		labels := svc.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		labels["skycluster.io/managed-by"] = "skycluster"
		
		// selectedProvNames contains the list of provider names selected for deployments
		// a namespace manifest must be created for each provider
		for _, pName := range selectedProvNames {
			// prepare service object
			newSvc := deepCopyService(svc, true)
			objMap, err := objToMap(newSvc)
			if err != nil {return nil, errors.Wrap(err, "error converting service to map.")}

			objMap["kind"] = "Service"
			objMap["apiVersion"] = "v1"
	
			name := "svc-" + newSvc.Name + "-" + pName
			rand := RandSuffix(name)
			name = name[0:int(math.Min(float64(len(name)), 20))]
			name = name + "-" + rand

			obj, err := generateObjectWrapper(name, newSvc.Namespace, objMap, provCfgNameMap[pName])
			if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }
			manifests = append(manifests, obj)
		}
	}

	return manifests, nil
}

// generateDeployManifests generates application manifests based on the deploy plan
// for distributed environment, including replicated deployments and services
func (r *SkyNetReconciler) generateDeployManifests(ns string, dpMap hv1a1.DeployMap, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
	// manifests := make([]hv1a1.SkyService, 0)
	manifests := make([]*hv1a1.SkyObject, 0)
	cmpnts := dpMap.Component
	edges := dpMap.Edges
	
	labels := derivePriorities(cmpnts, edges)

	// Deployments are created in selected remote clusters. We use "Object" CRD
	// to wrap the deployment object along with provider reference
	for _, deployItem := range cmpnts {
		// based on the type of services we may modify the objects' spec
		switch strings.ToLower(deployItem.ComponentRef.Kind) {
		case "deployment":
			// Also the deployment should be wrapped in "Object" which
			// the SkyApp controller can take care of it (for now).
			deploy := &appsv1.Deployment{}
			if err := r.Get(context.TODO(), client.ObjectKey{
				Namespace: ns, Name: deployItem.ComponentRef.Name,
			}, deploy); err != nil {return nil, errors.Wrap(err, "error getting Deployment.")}

			// Create a replicated deployment with node selector, labels, annotations
			// Discard all other fields that are not necessary
			newDeploy := generateNewDeplyFromDeploy(deploy)

			podLabels := labels[deployItem.ComponentRef.Name]
			podLabels["skycluster.io/managed-by"] = "skycluster"
			podLabels["skycluster.io/provider-name"] = deployItem.ProviderRef.Name

			lb := newDeploy.Spec.Template.ObjectMeta.Labels
			newDeploy.Spec.Template.ObjectMeta.Labels = lo.Assign(lb, podLabels) // merge
			
			// podLabels["skycluster.io/provider-region"] = deployItem.ProviderRef.Region
			// podLabels["skycluster.io/provider-zone"] = deployItem.ProviderRef.Zone
			// podLabels["skycluster.io/provider-platform"] = deployItem.ProviderRef.Platform
			// podLabels["skycluster.io/provider-region-alias"] = hv1a1.GetRegionAlias(deployItem.ProviderRef.RegionAlias)
			// podLabels["skycluster.io/provider-type"] = deployItem.ProviderRef.Type

			// spec.selector
			if newDeploy.Spec.Selector == nil {newDeploy.Spec.Selector = &metav1.LabelSelector{}}
			if newDeploy.Spec.Selector.MatchLabels == nil {newDeploy.Spec.Selector.MatchLabels = make(map[string]string)}
			// newDeploy.Spec.Selector.MatchLabels[pIdLabel] = providerId

			// Add general labels to the deployment
			deplLabels := newDeploy.ObjectMeta.Labels
			newDeplLabels := lo.Assign(deplLabels, labels[deployItem.ComponentRef.Name]) // merge
			newDeplLabels["skycluster.io/managed-by"] = "skycluster"
			newDeplLabels["skycluster.io/provider-name"] = deployItem.ProviderRef.Name

			newDeploy.ObjectMeta.Labels = lo.Assign(deplLabels, newDeplLabels)
			// deployLabels["skycluster.io/provider-region"] = deployItem.ProviderRef.Region
			// deployLabels["skycluster.io/provider-zone"] = deployItem.ProviderRef.Zone
			// deployLabels["skycluster.io/provider-platform"] = deployItem.ProviderRef.Platform
			// deployLabels["skycluster.io/provider-region-alias"] = hv1a1.GetRegionAlias(deployItem.ProviderRef.RegionAlias)
			// deployLabels["skycluster.io/provider-type"] = deployItem.ProviderRef.Type
			
			// yamlObj, err := generateYAMLManifest(newDeploy)
			objMap, err := objToMap(newDeploy)
			if err != nil {return nil, errors.Wrap(err, "error converting Deployment to map.")}
			
			objMap["kind"] = "Deployment"
			objMap["apiVersion"] = "apps/v1"

			name := "dp-" + newDeploy.Name + "-" + deployItem.ProviderRef.Name
			rand := RandSuffix(name)
			name = name[0:int(math.Min(float64(len(name)), 20))]
			name = name + "-" + rand

			obj, err := generateObjectWrapper(name, newDeploy.Namespace, objMap, provCfgNameMap[deployItem.ProviderRef.Name])
			if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }

			manifests = append(manifests, obj)
		}
	}

	return manifests, nil
}

// List all registered provider profiles
func (r *SkyNetReconciler) fetchProviderProfilesMap() (map[string]cv1a1.ProviderProfile, error) {
	ppList := &cv1a1.ProviderProfileList{}
	if err := r.List(context.Background(), ppList); err != nil {
		return nil, errors.Wrap(err, "listing provider profiles")
	}

	if len(ppList.Items) == 0 {
		return nil, errors.New("no provider profiles found")
	}

	providerProfilesMap := make(map[string]cv1a1.ProviderProfile)
	for _, pp := range ppList.Items {
		if _, ok := providerProfilesMap[pp.Spec.Platform]; !ok {
			providerProfilesMap[pp.Name] = pp
		}
	}
	return providerProfilesMap, nil
}


func derivePriorities(cmpnts []hv1a1.SkyService, edges []hv1a1.DeployMapEdge) map[string]map[string]string {

	// This function derives set of labels for each deployment target based on the edges
	// to respect the failover priority

	// e.g., 
	// Edges: A -> B (provider_b), and alternative deployment is available 
	// in C (provider_c) 

	// Source comes with label [as well as the destination rules]:
	//    failover-src-a-1: a-b
	//    failover-src-a-2: a-c

	// Then Deployment B must have labels:
	//    failover-src-a-1: a-b
	//    failover-src-a-2: a-c

	// Deployment C must have labels:
	//    failover-src-a-1: a-b

	// For a label to be considered for match, the previous labels must match, 
	// i.e. nth label would be considered matched only if first n-1 labels match.

	// Deployment A is labeled with its primary target provider
	// Then a backup target provider is found: another deployment that resides in 
	// same region/zone as the source deployment, if found; else any other deployment
	// that is not the primary target provider

	// Corresponding labels are added to the source and target deployment(s)
	// source: failover-src-a-1: a-b
	//         failover-src-a-2: a-c

	// target B: failover-src-a-1: a-b
	//           failover-src-a-2: a-c

	// target C: failover-src-a-1: a-b (only one)
	
	labels := make(map[string]map[string]string, 0)
	// dstForDeploy := make(map[string][]string)

	// ordered selected destination for each deployment in edges
	// The ordered list includes the primary target (selected by deploy plan)
	// and a backup plan (if any, prioritized based on region/zone proximity)

	// first, collect primary targets
	for _, edge := range edges {
		from := edge.From
		fromName := from.ComponentRef.Name
		fromProv := from.ProviderRef.Name
		to := edge.To
		toName := to.ComponentRef.Name
		toProv := to.ProviderRef.Name

		if _, ok := labels[fromName]; !ok {
			labels[fromName] = make(map[string]string)
		}
		if _, ok := labels[toName]; !ok {
			labels[toName] = make(map[string]string)
		}

		fName := RandSuffix(fromName + "-" + fromProv)
		tName := RandSuffix(toName + "-" + toProv)
		labelKey := "failover-" + fName + "-" + tName

		// must be added to both src and dst
		labels[fromName][labelKey] = fromName + "-" + fromProv + "-" + toName + "-" + toProv
		labels[toName][labelKey] = fromName + "-" + fromProv + "-" + toName + "-" + toProv
	}

	// then, collect backup targets
	for _, edge := range edges {
		from := edge.From
		fromName := from.ComponentRef.Name
		fromProv := from.ProviderRef.Name
		to := edge.To
		toName := to.ComponentRef.Name
		toProv := to.ProviderRef.Name

		// find backup target for 'from' deployment
		backups := lo.Filter(cmpnts, func(c hv1a1.SkyService, _ int) bool {
			return c.ComponentRef.Name == to.ComponentRef.Name && 
				c.ProviderRef.Name != to.ProviderRef.Name
		})
		
		// sort by the number of matching location attributes
		// score counts how many of the three fields match `from`.
		score := func(c hv1a1.SkyService) int {
			s := 0
			if c.ProviderRef.RegionAlias == from.ProviderRef.RegionAlias {s++}
			if c.ProviderRef.Region == from.ProviderRef.Region {s++}
			if c.ProviderRef.Zone == from.ProviderRef.Zone {s++}
			return s
		}

		sort.SliceStable(backups, func(i, j int) bool {
			// higher score => should come earlier
			return score(backups[i]) > score(backups[j])
		})

		if len(backups) == 0 {continue}
		bk := backups[0]
		bkName := bk.ComponentRef.Name
		bkProv := bk.ProviderRef.Name

		if _, ok := labels[bkName]; !ok {labels[bkName] = make(map[string]string)}

		fName := RandSuffix(fromName + "-" + fromProv)
		tName := RandSuffix(toName + "-" + toProv) // primary
		bName := RandSuffix(bkName + "-" + bkProv) 

		primaryKey := "failover-" + fName + "-" + tName // primary
		bkKey := "failover-" + fName + "-" + bName // backup
		
		// must add backup label to source
		labels[fromName][bkKey] = fromName + "-" + bkName + "-backup" // add to source
		// must add backup label to primary target
		labels[toName][bkKey] = fromName + "-" + bkName + "-backup" // add to primary target
		// must add primary target to the backup label 
		labels[bkName][primaryKey] = fromName + "-" + toName
	}
	return labels
}

func (r *SkyNetReconciler) generateIstioConfig(ns string, appId string, edges []hv1a1.DeployMapEdge, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
	manifests := make([]*hv1a1.SkyObject, 0)

	// Generate Istio configuration
	// As a general rule, we create a DestinationRule object that enforces
	// priorities for failover, and we prioritize the local provider.
	// More specifically, we priotize a destination where it adopts
	// as many labels as the client that send the request.
	// These set of labels are introduced in the DestinationRule object
	// and include provider region alias, region, and zone.
	for _, edge := range edges {
		// must fetch labels on the deployment and match with a service
		// then use service info to create DestinationRule object
		to := edge.To
		if strings.ToLower(to.ComponentRef.Kind) != "deployment" { continue }

		dep := &appsv1.Deployment{}
		if err := r.Get(context.TODO(), client.ObjectKey{
			Namespace: ns, Name: to.ComponentRef.Name,
		}, dep); err != nil {return nil, errors.Wrap(err, "error getting Deployment for edge target.")}

		// find the service that matches the deployment labels
		svcList := &corev1.ServiceList{}
		if err := r.List(context.TODO(), svcList, client.MatchingLabels{
			"skycluster.io/app-scope": "distributed",
			"skycluster.io/app-id":    appId,
		}); err != nil {return nil, errors.Wrap(err, "error listing Services for istio configuration.")}
		
		failovers := []string{
			"skycluster.io/provider-name", // highest priority, must match exactly, if not, the rest do not matter
			"skycluster.io/provider-platform",
			"skycluster.io/provider-region-alias",
			"skycluster.io/provider-region",
			"skycluster.io/provider-zone", // lowest priority, all above must match
		}

		for _, svc := range svcList.Items {
			if deploymentHasLabels(dep, svc.Spec.Selector) {
				
				ist := &istioClient.DestinationRule{
					ObjectMeta: metav1.ObjectMeta{
						Name: svc.Name + "-dst-rule",
						Namespace: svc.Namespace,
					},
					Spec: istiov1.DestinationRule{
						Host: svc.Name,
						TrafficPolicy: &istiov1.TrafficPolicy{
							LoadBalancer: &istiov1.LoadBalancerSettings{
								LbPolicy: &istiov1.LoadBalancerSettings_Simple{
									Simple: istiov1.LoadBalancerSettings_LEAST_REQUEST,
								},
								LocalityLbSetting: &istiov1.LocalityLoadBalancerSetting{
									FailoverPriority: failovers,
								},
							},
							OutlierDetection: &istiov1.OutlierDetection{
								ConsecutiveErrors:	5,
								Interval: 				 &duration.Duration{Seconds: 5},
								BaseEjectionTime: &duration.Duration{Seconds: 30},
								MaxEjectionPercent: 30,
							},
						},
					},
				}

				objMap, err := objToMap(ist)
				if err != nil {return nil, errors.Wrap(err, "error converting Deployment to map.")}
				
				objMap["kind"] = "DestinationRule"
				objMap["apiVersion"] = "networking.istio.io/v1"

				name := "dst-" + ist.Name + "-" + to.ProviderRef.Name
				rand := RandSuffix(name)
				name = name[0:int(math.Min(float64(len(name)), 20))]
				name = name + "-" + rand

				obj, err := generateObjectWrapper(name, svc.Namespace, objMap, provCfgNameMap[to.ProviderRef.Name])
				if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }

				manifests = append(manifests, obj)
				break
			}
		}
	}

	return manifests, nil
}

func generateObjectWrapper(name string, ns string, objAny map[string]any, providerConfigName string) (*hv1a1.SkyObject, error) {
	obj := &hv1a1.SkyObject{}
	obj.Name = name
	if ns != "" {obj.Namespace = ns}
	b, err := json.Marshal(objAny)
	if err != nil { return nil, errors.Wrap(err, "failed to marshal objectAny to JSON") }
	obj.Manifest = runtime.RawExtension{Raw: b}
	obj.ProviderRef = hv1a1.ProviderRefSpec{
		ConfigName: providerConfigName,
	}
	return obj, nil
}

func deepCopyDeployment(src appsv1.Deployment, clearAnnotations bool) *appsv1.Deployment {
	dest := src.DeepCopy()

	// Clear resource version and UID
	dest.ResourceVersion = ""
	dest.UID = ""					 
	dest.CreationTimestamp = metav1.Time{}
	dest.Generation = 0
	dest.ManagedFields = nil
	dest.OwnerReferences = nil
	dest.Finalizers = nil
	dest.Status = appsv1.DeploymentStatus{}
	if clearAnnotations {dest.Annotations = nil}

	return dest
}

func deepCopyService(src corev1.Service, clearAnnotations bool) *corev1.Service {
	dest := src.DeepCopy()

	// Clear resource version and UID
	dest.ResourceVersion = ""
	dest.UID = ""				 
	dest.CreationTimestamp = metav1.Time{}
	dest.Generation = 0
	dest.ManagedFields = nil
	dest.OwnerReferences = nil
	dest.Finalizers = nil
	dest.Status = corev1.ServiceStatus{}
	dest.Spec.ClusterIP = ""
	dest.Spec.ClusterIPs = nil
	
	dest.Spec.LoadBalancerIP = ""
	dest.Spec.LoadBalancerSourceRanges = nil
	dest.Spec.ExternalIPs = nil
	dest.Spec.ExternalName = ""
	
	if clearAnnotations {dest.Annotations = nil}

	return dest
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyNetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.SkyNet{}).
		Named("core-skynet").
		Complete(r)
}
