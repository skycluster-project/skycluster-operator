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

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

// SkyXRDReconciler reconciles a SkyXRD object
type SkyXRDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

type subnetMetadata struct {
	Type string `yaml:"type"`
	CIDR string `yaml:"cidr"`
}

type gatewayMetadata struct {
	Flavor     string `yaml:"flavor"`
	VolumeType string `yaml:"volumeType"`
	VolumeSize int    `yaml:"volumeSize"`
}

type providerMetadata struct {
	VPCCIDR string           `yaml:"vpcCidr"`
	Subnets []subnetMetadata `yaml:"subnets"`
	Gateway gatewayMetadata  `yaml:"gateway"`
}

// +kubebuilder:rbac:groups=core,resources=skyxrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=skyxrds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=skyxrds/finalizers,verbs=update

func (r *SkyXRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started")

	// Fetch the object
	skyxrd := &cv1a1.SkyXRD{}
	if err := r.Get(ctx, req.NamespacedName, skyxrd); err != nil {
		r.Logger.Info("SkyXRD not found.")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	manifests, _,  err := r.createManifests(req.Name, req.Namespace, skyxrd.Spec.DeployMap)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error creating manifests.")
	}
	
	skyxrd.Status.Manifests = manifests
	if err := r.Status().Update(ctx, skyxrd); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error updating SkyXRD status.")
	}
	
	return ctrl.Result{}, nil

	// for _, xrd := range skyxrd.Spec.Manifests {
	// 	var obj map[string]any
	// 	if err := yaml.Unmarshal([]byte(xrd.Manifest), &obj); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\tunable to unmarshal object", logName))
	// 		r.setConditionUnreadyAndUpdate(skyxrd, "unable to unmarshal object")
	// 		return ctrl.Result{}, err
	// 	}

	// 	unstrObj := &unstructured.Unstructured{Object: obj}
	// 	unstrObj.SetAPIVersion(xrd.ComponentRef.APIVersion)
	// 	unstrObj.SetKind(xrd.ComponentRef.Kind)
	// 	// The name contains "." which is not allowed when working with xrds
	// 	// We keep the orignial name as the Name field for SkyService object
	// 	// and replace "." with "-" for the object name when we need to work with XRDs
	// 	unstrObj.SetName(strings.Replace(xrd.ComponentRef.Name, ".", "-", -1))
	// 	if err := ctrl.SetControllerReference(skyxrd, unstrObj, r.Scheme); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\tunable to set controller reference", logName))
	// 		r.setConditionUnreadyAndUpdate(skyxrd, "unable to set controller reference")
	// 		return ctrl.Result{}, err
	// 	}

	// 	// if err := r.Create(ctx, unstrObj); err != nil {
	// 	// 	logger.Info(fmt.Sprintf("[%s]\tunable to create object, maybe it already exists?", logName))
	// 	// 	return ctrl.Result{}, client.IgnoreAlreadyExists(err)
	// 	// }
	// 	logger.Info(fmt.Sprintf("[%s]\tcreated object [%s]", logName, xrd.ComponentRef.Name))
	// }

	// r.setConditionReadyAndUpdate(skyxrd)
	// return ctrl.Result{}, nil
}

func (r *SkyXRDReconciler) generateProviderManifests(appId string, ns string, components []hv1a1.SkyService) (map[string]hv1a1.SkyService, error) {
	// We need to group the components based on the providers
	// and then generate the manifests for each provider
	// We then return the manifests for each provider
	uniqueProviders := make(map[string]hv1a1.ProviderRefSpec, 0)
	for _, cmpnt := range components {
		// ProviderName uniquely identifies a provider, set by the optimizer
		providerName := cmpnt.ProviderRef.Name
		if _, ok := uniqueProviders[providerName]; ok { continue }
		uniqueProviders[providerName] = cmpnt.ProviderRef
	}
	// Now we have all unique providers
	// We can now generate the manifests for each provider
	// we use settings corresponding to each provider stored in a configmap (ip ranges, etc.)
	provMetadata, err := r.loadProviderMetadata()
	if err != nil {
		return nil, errors.Wrap(err, "Error loading provider metadata.")
	}

	providerProfiles, err := r.fetchProviderProfilesMap()
	if err != nil {
		return nil, errors.Wrap(err, "Error fetching provider profiles.")
	}
	
	manifests := map[string]hv1a1.SkyService{}
	idx := 0
	for pName, p := range uniqueProviders {
		// idx += 1
		
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("skycluster.io/v1alpha1")
		obj.SetKind("XProvider")
		
		// obj.SetNamespace(ns)
		obj.SetName(pName)

		spec := map[string]any{
			"applicationId": appId,
			"vpcCidr":       provMetadata[p.Platform][idx].VPCCIDR,
			"gateway": func() map[string]any {
				if p.Platform == "openstack" || p.Platform == "baremetal" {
					return map[string]any{
						"flavor":     provMetadata[p.Platform][idx].Gateway.Flavor,
					}
				}
				return map[string]any{
					"flavor":     provMetadata[p.Platform][idx].Gateway.Flavor,
					"volumeType": provMetadata[p.Platform][idx].Gateway.VolumeType,
					"volumeSize": provMetadata[p.Platform][idx].Gateway.VolumeSize,
				}
			}(),
			"subnets": func() []map[string]any {
				subnets := make([]map[string]any, 0)
				for _, sn := range provMetadata[p.Platform][idx].Subnets {
					subnet := map[string]any{
						"cidr": sn.CIDR,
						"zone": p.Zone,
					}
					if p.Platform == "aws" {
						subnet["type"] = sn.Type
					}
					if p.Platform == "baremetal" && sn.Type == "public" {
						subnet["default"] = true
					} 
					subnets = append(subnets, subnet)
				}
				return subnets
			}(),
			"externalNetwork": func() map[string]string {
				if p.Platform != "openstack" {
					return nil
				}
				return map[string]string{
					"networkName": "ext-net",
					"networkId":   "0a23c4ae-98ba-4f3a-8c85-5a7dc614cc4e",
					"subnetName":  "ext-subnet",
					"subnetId":    "ae9a8eac-5f6f-4681-98f6-71acf18dcfbb",
				}
			}(),
			"providerRef": map[string]any{
				"name":   pName,
				"platform": p.Platform,
				"region": p.Region,
				"zones":  map[string]string{
					"primary": p.Zone,
					"secondary": findSecondaryZone(providerProfiles[pName], p.Zone),
				},
			},
		}
		obj.Object["spec"] = spec
		// extra labels only for "SAVI" (openstack)
		if p.Platform == "openstack" {
			objAnnt := make(map[string]string, 0)
			objAnnt["skycluster.io/external-resources"] = "[{\"apiVersion\":\"identity.openstack.crossplane.io/v1alpha1\",\"kind\":\"ProjectV3\",\"id\":\"1e1c724728544ec2a9058303ddc8f30b\"}]"
			obj.SetAnnotations(objAnnt)
		}
		
		yamlObj, err := generateYAMLManifest(obj)
		if err != nil {
			return nil, errors.Wrap(err, "Error generating YAML manifest.")
		}
		manifests[pName] = hv1a1.SkyService{
			ComponentRef: corev1.ObjectReference{
				APIVersion: obj.GetAPIVersion(),
				Kind:       obj.GetKind(),
				Namespace:  obj.GetNamespace(),
				Name:       obj.GetName(),
			},
			Manifest: yamlObj,
			ProviderRef: hv1a1.ProviderRefSpec{
				Name:   pName,
				Type: p.Type,
				Platform: p.Platform,
				Region: p.Region,
				RegionAlias: p.RegionAlias,
				Zone:   p.Zone,
			},
		}
	}
	return manifests, nil
}


func (r *SkyXRDReconciler) loadProviderMetadata() (map[string][]providerMetadata, error) {
	providerData, err := r.fetchProviderMetadataMap()
	if err != nil {
		return nil, errors.Wrap(err, "fetching provider metadata map")
	}

	providerMetadataByKey := make(map[string][]providerMetadata)
	for key, yamlStr := range providerData {
		var metas []providerMetadata
		if err := yaml.Unmarshal([]byte(yamlStr), &metas); err != nil {
			return nil, errors.Wrapf(err, "unmarshalling provider metadata for key %s", key)
		}
		providerMetadataByKey[key] = metas
	}

	return providerMetadataByKey, nil
}

func (r *SkyXRDReconciler) fetchProviderMetadataMap() (map[string]string, error) {
	cmList := &corev1.ConfigMapList{}
	if err := r.List(context.Background(), cmList, client.MatchingLabels{
		"skycluster.io/config-type": "provider-metadata-e2e",
	}); err != nil {
		return nil, errors.Wrap(err, "listing provider metadata ConfigMaps")
	}

	if len(cmList.Items) == 0 {
		return nil, errors.New("no provider metadata ConfigMaps found")
	}
	if len(cmList.Items) > 1 {
		return nil, errors.New("more than one provider metadata ConfigMap found")
	}

	providerData := make(map[string]string, len(cmList.Items[0].Data))
	for k, v := range cmList.Items[0].Data {
		providerData[k] = v
	}
	return providerData, nil
}

func (r *SkyXRDReconciler) fetchProviderProfilesMap() (map[string]cv1a1.ProviderProfile, error) {
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

func findSecondaryZone(p cv1a1.ProviderProfile, primary string) string {
	for _, z := range p.Spec.Zones {
		if z.Name != primary {
			return z.Name
		}
	}
	return ""
}

func (r *SkyXRDReconciler) createManifests(appId string, ns string, deployMap hv1a1.DeployMap) ([]hv1a1.SkyService, []hv1a1.SkyService, error) {
	manifests := make([]hv1a1.SkyService, 0)
	// ######### Providers
	// Each deployment comes with component info (e.g. kind and apiVersion and name)
	// as well as the provider info (e.g. name, region, zone, type) that it should be deployed on
	// We extract all provider's info and create corresponding SkyProvider objects for each provider
	providersManifests, err := r.generateProviderManifests(appId, ns, deployMap.Component)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error generating provider manifests.")
	}
	for _, obj := range providersManifests {
		manifests = append(manifests, obj)
		// skyObjs[skyObjName] = obj
	}

	// // ######### Deployments
	// allDeployments := make([]hv1a1.SkyService, 0)
	// for _, deployItem := range deployMap.Component {
	// 	// For each component we check its kind and based on that we decide how to proceed:
	// 	// 	If this is a Sky Service, then we create the corresponding Service (maybe just the yaml file?)
	// 	// 	If this is a Deployment, then we need to group the deployments based on the provider
	// 	// Then using decreasing first fit, we identitfy the number and type of VMs required.
	// 	// Then we create SkyK8SCluster with a controller and agents specified in previous step.
	// 	// We also need to annotate deployments carefully and create services and istio resources accordingly.

	// 	// based on the type of services we may modify the objects' spec
	// 	switch strings.ToLower(deployItem.ComponentRef.Kind) {
	// 	case "deployment":
	// 		// fmt.Printf("[Generate]\t Skipping manifest for Deployment [%s]...\n", deployItem.Component.Name)
	// 		allDeployments = append(allDeployments, deployItem)
	// 	case "vm":
	// 		skyObj, err := r.generateSkyVMManifest(ctx, req, deployItem)
	// 		if err != nil {
	// 			return nil, nil, errors.Wrap(err, "Error generating SkyVM manifest.")
	// 		}
	// 		manifests = append(manifests, *skyObj)
	// 	default:
	// 		// We only support above services for now...
	// 		return nil, nil, errors.New(fmt.Sprintf("unsupported component type [%s]. Skipping...\n", deployItem.ComponentRef.Kind))
	// 	}
	// }

	// // ######### Handle Deployments for SkyK8SCluster
	// skyK8SObj, err := r.generateSkyK8SCluster(ctx, req, allDeployments)
	// if err != nil {
	// 	return nil, nil, errors.Wrap(err, "Error generating SkyK8SCluster.")
	// }
	// manifests = append(manifests, *skyK8SObj)

	// // In addition to K8S cluster manfiests, we also generate application manifests
	// // (i.e. deployments, services, istio configurations, etc.) and
	// // submit them to the remote cluster using Kubernetes Provider (object)
	// // We create manifest and submit it to the SkyAPP controller for further processing
	// appManifests, err := r.generateSkyAppManifests(deployMap)
	// if err != nil {
	// 	return nil, nil, errors.Wrap(err, "Error generating SkyApp manifests.")
	// }

	// return manifests, appManifests, nil
	return manifests, nil, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyXRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.SkyXRD{}).
		Named("core-skyxrd").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}
