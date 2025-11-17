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
	"bytes"
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

// AtlasMeshReconciler reconciles a AtlasMesh object
type AtlasMeshReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

type orderedLabels struct {
	key   string
	value string
}

type priorityLabels struct {
	// sourceLabels are labels for the source component (deployment)
	// that are added to the target deployment's pod template
	// we need to keep track of them because these are added to DistinationRule
	// for failover configuration, and not all of labels
	sourceLabels map[string][]*orderedLabels
	// sourceLabels map[string]string
	// Each deployment has its own labels and labels that are added by others
	// to identify them as potential failover targets
	allLabels map[string]string
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=atlasmeshs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=atlasmeshs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=atlasmeshs/finalizers,verbs=update

func (r *AtlasMeshReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started")

	// Fetch the object
	var atlasmesh cv1a1.AtlasMesh
	if err := r.Get(ctx, req.NamespacedName, &atlasmesh); err != nil {
		r.Logger.Info("AtlasMesh resource not found. Ignoring since object must be deleted")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	provCfgNameMap, err := r.getProviderConfigNameMap(atlasmesh)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to get provider config name map")
	}
	r.Logger.Info("Fetched provider config name map.", "count", len(provCfgNameMap))

	appId, ok := atlasmesh.Labels["skycluster.io/app-id"]
	if !ok || appId == "" {
		return ctrl.Result{}, errors.New("missing required label: skycluster.io/app-id")
	}

	manifests := make([]*hv1a1.SkyObject, 0)

	// Must generate application manifests
	// (i.e. deployments, services, istio configurations, etc.) and
	// submit them to the remote cluster using Kubernetes Provider (object)
	// Manifest are submitted through "Object" CRD from Crossplane

	nsManifests, err := r.generateNamespaceManifests(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap.Component, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate namespace manifests") }
	manifests = append(manifests, nsManifests...)

	cdManifests, err := r.generateConfigDataManifests(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap.Component, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate config data manifests") }
	manifests = append(manifests, cdManifests...)

	depManifests, err := r.generateDeployManifests(atlasmesh.Namespace, atlasmesh.Spec.DeployMap, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate application manifests") }
	manifests = append(manifests, depManifests...)

	svcManifests, err := r.generateServiceManifests(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap.Component, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate service manifests") }
	manifests = append(manifests, svcManifests...)

	manifestsIstio, err := r.generateIstioConfig(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap, provCfgNameMap)
	if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to generate istio configuration manifests") }
	manifests = append(manifests, manifestsIstio...)

	for _, depObj := range manifests {
		// obj, _ := generateUnstructuredWrapper(depObj, &atlasmesh, r.Scheme)
		__obj := map[string]any{}
		__obj["provider"] = depObj.ProviderRef.ConfigName
		__obj["manifest"], err = depObj.ManifestAsMap()
		if err != nil { return ctrl.Result{}, errors.Wrap(err, "failed to convert manifest to map for writing to file") }
		utils.WriteMapToFile(&__obj, "/tmp/istio2/"+depObj.Name+".yaml")
	}

	// manifests = append(manifests, manifestsIstio...)

	oldMap := make(map[string]hv1a1.SkyObject, len(atlasmesh.Status.Objects))
	for _, o := range atlasmesh.Status.Objects {oldMap[o.Name] = o}

	newObjects := make([]hv1a1.SkyObject, 0, len(manifests))
	for _, m := range manifests {newObjects = append(newObjects, *m)}	
	
	changed := false
	r.Logger.Info("Reconciling AtlasMesh manifests.", "oldCount", len(oldMap), "newCount", len(newObjects))

	if len(oldMap) != len(newObjects) {
		changed = true
	} else {
		for _, m := range newObjects {
			oldObj, ok := oldMap[m.Name]; 
			if !ok {changed = true; break}
			if !reflect.DeepEqual(oldObj.Manifest, m.Manifest) {
				r.Logger.Info("Manifest object spec differs.", "name", m.Name)
				changed = true; break
			}
		}
	}

	if changed {
		atlasmesh.Status.Objects = newObjects
		if err := r.Status().Update(ctx, &atlasmesh); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "error updating AtlasMesh status")
		}
		r.Logger.Info("Updated AtlasMesh status objects.", "count", len(atlasmesh.Status.Objects))
	}

	// TODO: must check for deleted manifests and delete them from the remote clusters
	// For now, we only support adding new manifests
	if atlasmesh.Spec.Approve {
		r.Logger.Info("Approval given, creating manifest objects in the cluster.")
		for _, m := range manifests {
			obj, err := generateUnstructuredWrapper(m, &atlasmesh, r.Scheme)
			if err != nil {return ctrl.Result{}, errors.Wrap(err, "failed to generate unstructured wrapper")}

			// Write the objects to disk for debugging
			utils.WriteObjectToFile(obj, "/tmp/manifests/app-"+obj.GetName()+".yaml")
			
			if err := r.Create(ctx, obj); err != nil {
				if apierrors.IsAlreadyExists(err) {
					// r.Logger.Info("Manifest object already exists, checking for updates.", "name", obj.GetName())
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

					curManifest, err1 := utils.GetNestedValue(current.Object, "spec", "manifest")
					objManifest, err2 := utils.GetNestedValue(obj.Object, "spec", "manifest")
					if err1 != nil || err2 != nil {
						return ctrl.Result{}, errors.Wrap(err, "error getting current manifest")
					}
					if !equalManifests(curManifest, objManifest) {
						r.Logger.Info("Manifest object spec differs, updating object.", "name", obj.GetName())
						// We only expect the provider config and manifest to change
						// TODO: handle provider config change later
						current.Object["spec"].(map[string]any)["manifest"] = objManifest
						if err := r.Update(ctx, current); err != nil { return ctrl.Result{}, errors.Wrap(err, "error updating manifest object") }
					}
				} else { return ctrl.Result{}, errors.Wrap(err, "error creating manifest object") }
			}
		}
	}
	return ctrl.Result{}, nil
}

func generateUnstructuredWrapper(skyObj *hv1a1.SkyObject, owner *cv1a1.AtlasMesh, scheme *runtime.Scheme) (*unstructured.Unstructured, error) {
	
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

func equalManifests(a, b interface{}) bool {
    aJSON, _ := json.Marshal(a)
    bJSON, _ := json.Marshal(b)
    return bytes.Equal(aJSON, bJSON)
}

// This is the name of corresponding XKube "Object" provider config (the remote k8s cluster)
// it returns a map of provider profile name to provider config name
// the local cluster is mapped to "local" key
func (r *AtlasMeshReconciler) getProviderConfigNameMap(atlasmesh cv1a1.AtlasMesh) (map[string]string, error) {
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
func (r *AtlasMeshReconciler) generateConfigDataManifests(ns string, appId string, cmpnts []hv1a1.SkyService, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {

	manifests := make([]*hv1a1.SkyObject, 0)
	selectedProvNames := make([]string, 0)

	// Since we don't know and do not care about the location of configmaps,
	// we have them distributed to all clusters in the distributed setup
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
		}
	}
	selectedProvNames = lo.Uniq(selectedProvNames)

	labels := map[string]string{
		"skycluster.io/app-scope": "distributed",
		"skycluster.io/app-id":    appId,
	}

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
			// prepare configmap object
			newCm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmObj.Name,
					Namespace: cmObj.Namespace,
					Labels:   labels,
				},
				Data: cmObj.Data,
			}
			objMap, err := objToMap(newCm)
			if err != nil {
				return nil, errors.Wrap(err, "error converting ConfigMap to map.")
			}

			objMap["kind"] = "ConfigMap"
			objMap["apiVersion"] = "v1"

			name := "cm-" + newCm.Name + "-" + pName
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
			// prepare secret object
			newSec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secObj.Name,
					Namespace: secObj.Namespace,
					Labels:   labels,
				},
				Data: secObj.Data,
			}
			objMap, err := objToMap(newSec)
			if err != nil {
				return nil, errors.Wrap(err, "error converting Secret to map.")
			}

			objMap["kind"] = "Secret"
			objMap["apiVersion"] = "v1"

			name := "sec-" + newSec.Name + "-" + pName
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

func (r *AtlasMeshReconciler) generateNamespaceManifests(ns string, appId string, cmpnts []hv1a1.SkyService, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
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

func (r *AtlasMeshReconciler) generateServiceManifests(ns string, appId string, cmpnts []hv1a1.SkyService, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
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
		labels["skycluster.io/service-name"] = svc.Name

		
		// selectedProvNames contains the list of provider names selected for deployments
		// a service manifest must be created for each provider
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
func (r *AtlasMeshReconciler) generateDeployManifests(ns string, dpMap hv1a1.DeployMap, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
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

			deployItemUniqName := deployItem.ComponentRef.Name + "-" + deployItem.ProviderRef.Name
			// pod labels get the all labels (its own + added by others for priority/failover)
			allLabels := labels[deployItemUniqName].allLabels
			
			// sourcelabels key is the destination deployment for the current deployment
			// and the values are labels added to the destination
			// These labels must be added to the source deployment's pod template
			// sourceLabels := labels[deployItemUniqName].sourceLabels
			
			// annotations (for istio custom buckets)
			podAnts := newDeploy.Spec.Template.ObjectMeta.Annotations
			newDeploy.Spec.Template.ObjectMeta.Annotations = lo.Assign(podAnts,
				map[string]string{
					"sidecar.istio.io/statsHistogramBuckets": "{\"istiocustom\":[1,2,4,6,8,10,12,14,16,18,20,22,24,26,28,30,32,34,36,38,40,43,46,49,52,55,58,62,66,70,75,80,85,90,95,100,105,110,115,120,125,130,135,140,145,150,155,160,170,180,190,200,210,220,230,240,250,260,270,280,290,300,320,340,360,380,390,400,420,440,460,480,500,550,600,650,700,750,800,850,900,950,1000,1100,1200,1300,1400,1500,1600,1700,1800,1900,2000,2500,3000,3500,4000,5000,6000,8000,10000,15000,20000,30000,40000,50000,75000,100000,300000,600000,1800000,3600000]}",
				},
			)	

			lb := lo.Assign(
				newDeploy.Spec.Template.ObjectMeta.Labels, 
				map[string]string{
					"skycluster.io/managed-by": "skycluster",
					"skycluster.io/component": deployItemUniqName,
				},
				allLabels,
			)

			// sort the labels for consistency
			keys := make([]string, 0, len(lb))
			for k := range lb { keys = append(keys, k)}
			sort.Strings(keys)

			// pod labels
			newDeploy.Spec.Template.ObjectMeta.Labels = func () map[string]string {
				sortedMap := make(map[string]string, len(lb))
				for _, k := range keys {sortedMap[k] = lb[k]}
				return sortedMap
			}()
			// for _, srcLbls := range sourceLabels {
			// 	maps.Copy(newDeploy.Spec.Template.ObjectMeta.Labels, srcLbls)
			// } 

			// deployment spec.selector
			if newDeploy.Spec.Selector == nil {newDeploy.Spec.Selector = &metav1.LabelSelector{}}
			if newDeploy.Spec.Selector.MatchLabels == nil {newDeploy.Spec.Selector.MatchLabels = make(map[string]string)}

			newDeploy.Spec.Selector.MatchLabels = lo.Assign(
				newDeploy.Spec.Selector.MatchLabels,
				map[string]string{
					"skycluster.io/component": deployItemUniqName,
					"skycluster.io/managed-by": "skycluster",
				},
			)

			// Add general labels to the deployment
			deplLabels := newDeploy.ObjectMeta.Labels
			newDeploy.ObjectMeta.Labels = lo.Assign(deplLabels, 
				map[string]string{
					"skycluster.io/component": deployItemUniqName,
					"skycluster.io/managed-by": "skycluster",
				},
			) 
			
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
func (r *AtlasMeshReconciler) fetchProviderProfilesMap() (map[string]cv1a1.ProviderProfile, error) {
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

func derivePriorities(cmpnts []hv1a1.SkyService, edges []hv1a1.DeployMapEdge) map[string]*priorityLabels {
	
	// using Istio destination rule, we control which target endpoint is used for each deployment
	// We use failover priority that enables selection of endpoints based on the 
	// matching labels provided in DestionaRule and target pod labels
	
	// We use VirtualService and DestinationRule to control the traffic routing
	
	// VirtualService defines the routing rules to the target service endpoints
	// DestinationRule defines the failover priorities among the target service endpoints
	
	// For each target service endpoint, we define a virtual service and destination rule
	// For each source deployment, we define a HTTPRoute in the virtual service
	// that points to the a route destination for this deployment
	
	// Within the destination rule, we define the failover priorities for each 
	// route destination defined in the virtual service
	
	
	// The labels in DestinationRule must match with the labels in the target pods
	// The target pod that matches the largest number of labels is selected.
	// For a match to be considered, all previous labels must match as well.
	
	// The plan for selecting target deployment then is as follows:
	// For each src deployment -> target deployment as selected by deployment plan
	// We add same labels to target deployment and DestinationRule for target service endpoint
	// (in source deployment cluster)
	
	// For a backup target deployment (if any, typically same zone/region as source deployment):
	// we add additional labels to the backup target, BUT we also need to add this label 
	// to the primary target deployment as well. Since a target with largest number of matching
	// labels is selected, the primary target will be selected first. The backup target
	// will be selected only if the primary target is not available.
	
	// Here is an example:
	// Source deployment A (in cluster/provider_1) targets deployment X (in cluster/provider_2)
	// Source deployment B (in cluster/provider_1) also targets deployment X
	// Backup target for deployment A and B is deployment Y (in cluster/provider_3)
	//
	// The derived labels would be as follows:
	
	// Virtual Service for X in cluster/provider_1:
	//  - match: deployment A labels
	//    route to: destination rule X, subset A
	//  - match: deployment B labels
	//    route to: destination rule X, subset B
	
	// DestinationRule for service X in cluster_1: 
	//  - subset A:
	//    trafficPolicy:
	//      loadBalancer:
	//        failoverPriority:
	//          - failover-src-a-1: a-x
	//          - failover-src-a-2: a-y
	//  - subset B:
	//    trafficPolicy:
	//      loadBalancer:
	//        failoverPriority:
	//          - failover-src-b-1: b-x
	//          - failover-src-b-2: b-y
	
	// Deployment X:
	//    failover-src-a-1: a-x
	//    failover-src-a-2: a-y
	//    failover-src-b-1: b-x
	//    failover-src-b-2: b-y
	// Deployment A:
	//    failover-src-a-1: a-x
	//    failover-src-a-2: a-y
	// Deployment B:
	//    failover-src-b-1: b-x
	//    failover-src-b-2: b-y
	// Deployment Y:
	//    failover-src-a-2: a-y
	//    failover-src-b-2: b-y

	
	labels := make(map[string]*priorityLabels)

	// ordered selected destination for each deployment in edges
	// The ordered list includes the primary target (selected by deploy plan)
	// and a backup plan (if any, prioritized based on region/zone proximity)

	// first, collect primary targets
	for _, edge := range edges {
		from := edge.From
		fromName := from.ComponentRef.Name + "-" + from.ProviderRef.Name
		to := edge.To
		toName := to.ComponentRef.Name + "-" + to.ProviderRef.Name

		if _, ok := labels[fromName]; !ok {
			labels[fromName] = &priorityLabels{
				sourceLabels: make(map[string][]*orderedLabels),
				allLabels:    make(map[string]string),
			}
		}
		if _, ok := labels[toName]; !ok {
			labels[toName] = &priorityLabels{
				sourceLabels: make(map[string][]*orderedLabels),
				allLabels:    make(map[string]string),
			}
		}

		// fName := RandSuffix(fromName)
		// tName := RandSuffix(toName)
		// labelKey := "failover-" + fName + "-" + tName
		labelKey := "failover/" + fromName + "-" + toName
		labelKey = ShortenLabelKey(labelKey)

		// must be added to both src and dst
		// for source as source label and for destination as all label
		// sourceLabels uses toName as key to filter labels for a specific target
		// when there are multiple targets from the same source
		//   e.g., source A targets X and Y
		if labels[fromName].sourceLabels[toName] == nil {
			labels[fromName].sourceLabels[toName] = make([]*orderedLabels, 0)
		}

		labels[fromName].sourceLabels[toName] = append(labels[fromName].sourceLabels[toName], &orderedLabels{
			key:   labelKey,
			value: ShortenLabelKey(fromName + "-" + toName),
		})

		labels[toName].allLabels[labelKey] = ShortenLabelKey(fromName + "-" + toName)
		// TODO: this must be added to the backup as well
		// TODO: backup label must be added to the primary target as well
		
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
			// higher score first; if equal, tie-break by ProviderRef.Name for stability
			si, sj := score(backups[i]), score(backups[j])
			if si == sj {
				return backups[i].ProviderRef.Name < backups[j].ProviderRef.Name
			}
			return si > sj
		})

		if len(backups) == 0 {continue}
		bk := backups[0]
		bkName := bk.ComponentRef.Name + "-" + bk.ProviderRef.Name

		if _, ok := labels[bkName]; !ok {
			labels[bkName] = &priorityLabels{
				sourceLabels: make(map[string][]*orderedLabels),
				allLabels:    make(map[string]string),
			}
		}

		// fName := RandSuffix(fromName)
		// bName := RandSuffix(bkName) 
		// bkKey := "failover-" + fName + "-" + bName // backup
		bkKey := "failover/" + fromName + "-" + bkName // backup
		bkKey = ShortenLabelKey(bkKey)
		
		// must add backup label to source (as source labels)
		if labels[fromName].sourceLabels[toName] == nil {
			labels[fromName].sourceLabels[toName] = make([]*orderedLabels, 0)
		}
		labels[fromName].sourceLabels[toName] = append(labels[fromName].sourceLabels[toName], &orderedLabels{
			key:   bkKey,
			value: ShortenLabelKey(fromName + "-" + bkName) + "-backup",
		})
		// must add backup label to primary target (as all labels)
		labels[toName].allLabels[bkKey] = ShortenLabelKey(fromName + "-" + bkName) + "-backup" // add to primary target

		// backup only gets the primary target label and not the backup (hence it has fewer labels)
		labels[bkName].allLabels[labelKey] = ShortenLabelKey(fromName + "-" + toName)
	}
	return labels
}

func (r *AtlasMeshReconciler) generateIstioConfig(ns string, appId string, dpMap hv1a1.DeployMap, provCfgNameMap map[string]string) ([]*hv1a1.SkyObject, error) {
	manifests := make([]*hv1a1.SkyObject, 0)

	edges := dpMap.Edges
	cmpnts := dpMap.Component

	// failover priority labels
	// a map of (ComponentRef.Name + "-" + ProviderRef.Name) to priorityLabels
	labels := derivePriorities(cmpnts, edges)

	// Generate Istio configuration
	// We use VirtualService and DestinationRule to control the traffic routing
	
	// VirtualService defines the routing rules to the target service endpoints
	// DestinationRule defines the failover priorities among the target service endpoints
	
	// For each target service endpoint, we define a virtual service and destination rule
	// For each source deployment, we define a HTTPRoute in the virtual service
	// that points to the a route destination for this deployment
	
	// Within the destination rule, we define the failover priorities for each 
	// route destination defined in the virtual service
	type virtualServiceHelper struct {
		ProviderCfgName string
		VirtualService  *istioClient.VirtualService
	}
	type dstRuleHelper struct {
		ProviderCfgName string
		DstRule         *istioClient.DestinationRule
	}

	virtualSvcs := make(map[string]*virtualServiceHelper)
	dstRules := make(map[string]*dstRuleHelper)
	
	for _, edge := range edges {
		// must fetch labels on the deployment and match with a service
		// then use service info to create/update virtual service and destination rule
		from := edge.From
		fromName := from.ComponentRef.Name + "-" + from.ProviderRef.Name
		to := edge.To
		// toName := to.ComponentRef.Name + "-" + to.ProviderRef.Name
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

		for _, svc := range svcList.Items {
			if deploymentHasLabels(dep, svc.Spec.Selector) {

				// Making a virtual service and destination rule for this service
				// in the source deployment's cluster (from.ProviderRef)
				name := svc.Name + "-" + from.ProviderRef.Name
				if _, ok := virtualSvcs[name]; !ok {
					virtualSvcs[name] = &virtualServiceHelper{
						ProviderCfgName: from.ProviderRef.Name,
						VirtualService: &istioClient.VirtualService{
							ObjectMeta: metav1.ObjectMeta{
								Name:      name,
								Namespace: svc.Namespace,
								Labels: map[string]string{
									"skycluster.io/service-name": svc.Name,
								},
							},
							Spec: istiov1.VirtualService{
								Hosts:    []string{svc.Name},
							},
						},
					}
				}
				// init destination rule
				if _, ok := dstRules[name]; !ok { 
					dstRules[name] = &dstRuleHelper{
						ProviderCfgName: from.ProviderRef.Name,
						DstRule: &istioClient.DestinationRule{
							ObjectMeta: metav1.ObjectMeta{
								Name:      name,
								Namespace: svc.Namespace,
							},
							Spec: istiov1.DestinationRule{
								Host: svc.Name,
							},
					  },
					}
				}

				// Now per each source deployment (from), we add a HTTPRoute to the virtual service
				// Check Virtual Service example
				vs := virtualSvcs[name]
				http := &istiov1.HTTPRoute{
					Name: from.ComponentRef.Name,
					Match: []*istiov1.HTTPMatchRequest{
						{
							// must match the source deployment
							SourceLabels: map[string]string{
								"skycluster.io/component": fromName,
							},
						},
					},
					Route: []*istiov1.HTTPRouteDestination{
						{
							Destination: &istiov1.Destination{
								Host: svc.Name,
								Subset: from.ComponentRef.Name,
							},
						},
					},
				}
				
				vs.VirtualService.Spec.Http = append(vs.VirtualService.Spec.Http, http)
				
				labelAndKeys := make([]string, 0)
				for _, kv := range labels[fromName].sourceLabels[to.ComponentRef.Name + "-" + to.ProviderRef.Name] {
					labelAndKeys = append(labelAndKeys, kv.key + "=" + kv.value)
				}

				// Now per each source deployment (from), we add a subset to the destination rule
				// Check Destination Rule example
				dr := dstRules[name]
				subset := &istiov1.Subset{
					Name: from.ComponentRef.Name,
					// Labels: map[string]string{
					// 	"skycluster.io/component": fromName,
					// },
					TrafficPolicy: &istiov1.TrafficPolicy{
						LoadBalancer: &istiov1.LoadBalancerSettings{
							LbPolicy: &istiov1.LoadBalancerSettings_Simple{
								Simple: istiov1.LoadBalancerSettings_LEAST_REQUEST,
							},
							LocalityLbSetting: &istiov1.LocalityLoadBalancerSetting{
								// failover labels, from this source only
								FailoverPriority: labelAndKeys,
							},
						},
						OutlierDetection: &istiov1.OutlierDetection{
							ConsecutiveErrors:	5,
							Interval: 				 &duration.Duration{Seconds: 5},
							BaseEjectionTime: &duration.Duration{Seconds: 30},
							MaxEjectionPercent: 30,
						},
					},
				}
				dr.DstRule.Spec.Subsets = append(dr.DstRule.Spec.Subsets, subset)

				break // found the matching service, do not expect to have multiple matches
			}
		} 
		
		// svc has found and we have created/updated virtual service and destination rule
	}

	for _, vs := range virtualSvcs {
		objMap, err := objToMap(vs.VirtualService)
		if err != nil {return nil, errors.Wrap(err, "error converting VirtualService to map.")}
		
		objMap["kind"] = "VirtualService"
		objMap["apiVersion"] = "networking.istio.io/v1beta1"

		name := "vs-" + vs.VirtualService.Name // svc.Name + "-" + from.ProviderRef.Name
		rand := RandSuffix(name)
		name = name[0:int(math.Min(float64(len(name)), 25))]
		name = name + "-" + rand

		obj, err := generateObjectWrapper(name, vs.VirtualService.Namespace, objMap, provCfgNameMap[vs.ProviderCfgName])
		if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }

		manifests = append(manifests, obj)
	}

	for _, dr := range dstRules {
		objMap, err := objToMap(dr.DstRule)
		if err != nil {return nil, errors.Wrap(err, "error converting DestinationRule to map.")}
		
		objMap["kind"] = "DestinationRule"
		objMap["apiVersion"] = "networking.istio.io/v1beta1"

		name := "dr-" + dr.DstRule.Name // svc.Name + "-" + from.ProviderRef.Name
		rand := RandSuffix(name)
		name = name[0:int(math.Min(float64(len(name)), 25))]
		name = name + "-" + rand

		obj, err := generateObjectWrapper(name, dr.DstRule.Namespace, objMap, provCfgNameMap[dr.ProviderCfgName])
		if err != nil { return nil, errors.Wrap(err, "error generating object wrapper.") }

		manifests = append(manifests, obj)
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
func (r *AtlasMeshReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.AtlasMesh{}).
		Named("core-atlasmesh").
		Complete(r)
}
