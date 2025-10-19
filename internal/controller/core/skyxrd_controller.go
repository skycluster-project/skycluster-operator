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
	"math"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
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

type PodCidrSpec struct {
	Cidr 	string `yaml:"cidr"`
	Public string `yaml:"public,omitempty"`
	Private string `yaml:"private,omitempty"`
}

type k8sMetadata struct {
	ServiceCidr string `yaml:"serviceCidr,omitempty"`
	NodeCidr    string `yaml:"nodeCidr,omitempty"`
	PodCidr     PodCidrSpec `yaml:"podCidr,omitempty"`
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

	manifests, _,  err := r.createManifests(req.Name, req.Namespace, skyxrd)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error creating manifests.")
	}
	
	skyxrd.Status.Manifests = manifests
	if err := r.Status().Update(ctx, skyxrd); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error updating SkyXRD status.")
	}
	
	if skyxrd.Spec.Approve {
		for _, xrd := range manifests {
			var obj map[string]any
			if err := yaml.Unmarshal([]byte(xrd.Manifest), &obj); err != nil {
				r.Logger.Error(err, "error unmarshalling manifest yaml.", "manifest", xrd.Manifest)
				return ctrl.Result{}, err
			}
	
			unstrObj := &unstructured.Unstructured{Object: obj}
			unstrObj.SetAPIVersion(xrd.ComponentRef.APIVersion)
			unstrObj.SetKind(xrd.ComponentRef.Kind)
			unstrObj.SetName(xrd.ComponentRef.Name)
			if err := ctrl.SetControllerReference(skyxrd, unstrObj, r.Scheme); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "error setting controller reference.")
			}
	
			// if err := r.Create(ctx, unstrObj); err != nil {
			// 	logger.Info(fmt.Sprintf("[%s]\tunable to create object, maybe it already exists?", logName))
			// 	return ctrl.Result{}, client.IgnoreAlreadyExists(err)
			// }
			r.Logger.Info("Created SkyXRD object!", "name", xrd.ComponentRef.Name)
		}
	}

	skyxrd.Status.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "Reconciled", "SkyXRD reconciled successfully.")
	if err := r.Status().Update(ctx, skyxrd); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error updating SkyXRD status after approval.")
	}
	return ctrl.Result{}, nil
}

func (r *SkyXRDReconciler) createManifests(appId string, ns string, skyxrd *cv1a1.SkyXRD) ([]hv1a1.SkyService, []hv1a1.SkyService, error) {
	deployMap := skyxrd.Spec.DeployMap
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
	}

	// ######### Deployments
	// allDeployments := make([]hv1a1.SkyService, 0)
	// for _, deployItem := range deployMap.Component {
		// For each component we check its kind and based on that we decide how to proceed:
		// 	If this is a Sky Service, then we create the corresponding Service (maybe just the yaml file?)
		// 	If this is a Deployment, then we need to group the deployments based on the provider
		// Then using decreasing first fit, we identitfy the number and type of VMs required.
		// Then we create SkyK8SCluster with a controller and agents specified in previous step.
		// We also need to annotate deployments carefully and create services and istio resources accordingly.

	// based on the type of services we may modify the objects' spec
	// 	switch strings.ToLower(deployItem.ComponentRef.Kind) {
	// 	case "deployment":
	// 		allDeployments = append(allDeployments, deployItem)
	// 	case "vm":
	// 		skyObj, err := r.generateSkyVMManifest(ctx, req, deployItem)
	// 		if err != nil {
	// 			return nil, nil, errors.Wrap(err, "Error generating SkyVM manifest.")
	// 		}
	// 		manifests = append(manifests, *skyObj)
	// 	default:
	//  	 We only support above services for now...
	// 		return nil, nil, errors.New(fmt.Sprintf("unsupported component type [%s]. Skipping...\n", deployItem.ComponentRef.Kind))
	// 	}
	// }

	// providerProfiles, err := r.fetchProviderProfilesMap()
	// if err != nil {return nil, nil, errors.Wrap(err, "Error fetching provider profiles.")}

	depPolicy, err := r.fetchDeploymentPolicy(ns, skyxrd.Spec.DeploymentPolicyRef)
	if err != nil { return nil, nil, errors.Wrap(err, "fetching deployment policy") }

	requiredVirtSvcs := make(map[string][]pv1a1.VirtualServiceSelector)
	for _, skySrvc := range deployMap.Component {
		pName := skySrvc.ProviderRef.Name
		if _, ok := requiredVirtSvcs[pName]; !ok {
			requiredVirtSvcs[pName] = make([]pv1a1.VirtualServiceSelector, 0)
		}

		svcs, err := r.findReqVirtualSvcs(ns, skySrvc, *depPolicy)
		if err != nil {return nil, nil, errors.Wrap(err, "fetching required virtual services")}

		requiredVirtSvcs[pName] = append(requiredVirtSvcs[pName], svcs...)
	}

	// we now know the required virtual services for each provider
	managedK8sSvcs := make(map[string][]pv1a1.VirtualServiceSelector)
	for pName, virtSvcs := range requiredVirtSvcs {
		for _, vs := range virtSvcs {
			switch vs.Kind {
			case "ManagedKubernetes":
				// we only need to create one ManagedKubernetes virtual service per provider
				// we filter all ComputeProfile (flavors) and use them to create workers in the cluster
				if _, ok := managedK8sSvcs[pName]; !ok {
					managedK8sSvcs[pName] = lo.Filter(virtSvcs, func(v pv1a1.VirtualServiceSelector, _ int) bool {
						return v.Kind == "ComputeProfile"
					})
				}
			case "ComputeProfile":
				// TODO: Handle ComputeProfile virtual services
				r.Logger.Info("ComputeProfile virtual service found. Skipping...", "provider", pName, "service", vs.Name)
			default:
				return nil, nil, errors.New("unsupported virtual service kind: " + vs.Kind)
			}
		}
	}

	// Handle ManagedKubernetes virtual services
	// managedK8sSvcs contains ComputeProfile (flavors) for the cluster
	managedK8sManifests, err := r.generateMgmdK8sManifests(appId, ns, managedK8sSvcs)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error generating SkyK8SCluster.")
	}
	for _, obj := range managedK8sManifests {
		manifests = append(manifests, obj)
	}

	// Handle unmanaged K8S Cluster (using VMs)
	// skyK8SObj, err := r.generateSkyK8SCluster(ctx, req, allDeployments)
	// if err != nil {
	// 	return nil, nil, errors.Wrap(err, "Error generating SkyK8SCluster.")
	// }
	// manifests = append(manifests, *skyK8SObj)

	return manifests, nil, nil
}

// Creates manifest for setup of providers needed for the deployment
func (r *SkyXRDReconciler) generateProviderManifests(appId string, ns string, cmpnts []hv1a1.SkyService) (map[string]hv1a1.SkyService, error) {
	// We need to group the components based on the providers
	// and then generate the manifests for each provider
	// We then return the manifests for each provider
	uniqueProviders := make(map[string]hv1a1.ProviderRefSpec, 0)
	for _, cmpnt := range cmpnts {
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
	
	provLastIdxUsed := make(map[string]int)
	manifests := map[string]hv1a1.SkyService{}
	for pName, p := range uniqueProviders {
		if _, ok := provLastIdxUsed[pName]; !ok {provLastIdxUsed[pName] = 0}
		idx := provLastIdxUsed[pName]

		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("skycluster.io/v1alpha1")
		obj.SetKind("XProvider")
		
		// obj.SetNamespace(ns)
		obj.SetName(pName)

		spec := map[string]any{
			"applicationId": appId,
			"vpcCidr":       provMetadata[p.Platform][idx].VPCCIDR,
			"gateway": func() map[string]any {
				fields := make(map[string]any)
				if p.Platform == "aws" {
					fields["volumeType"] = "gp2"
					fields["volumeSize"] = 30
				}
				fields["flavor"] = provMetadata[p.Platform][idx].Gateway.Flavor
				return fields
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
		provLastIdxUsed[pName] = idx + 1
	}
	return manifests, nil
}

func (r *SkyXRDReconciler) generateMgmdK8sManifests(appId string, ns string, svcList map[string][]pv1a1.VirtualServiceSelector) (map[string]hv1a1.SkyService, error) {
	// For managed K8S deployments, we start the cluster with nodes needed for control plane
	// and let the cluster autoscaler handle the rest of the scaling for the workload.
	// The optimization has decided that for requested workload, this provider is the best fit.

	k8sMetadata, err := r.loadK8sSrvcMetadata()
	if err != nil {return nil, errors.Wrap(err, "Error loading provider metadata.")}

	provProfiles, err := r.fetchProviderProfilesMap()
	if err != nil {return nil, errors.Wrap(err, "Error fetching provider profiles.")}

	manifests := map[string]hv1a1.SkyService{}
	provLastIdxUsed := make(map[string]int)

	for pName, computeProfileList := range svcList {
		// computeProfileList is the list of ComputeProfile virtual services for this provider
		if _, ok := provLastIdxUsed[pName]; !ok {provLastIdxUsed[pName] = 0}
		idx := provLastIdxUsed[pName]

		pp := provProfiles[pName]
		znPrimary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
		if !ok {return nil, errors.New("No primary zone found")}
		znSecondary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.Name != znPrimary.Name })
		if !ok {return nil, errors.New("No secondary zone found")}

		// we need to create a XKube object
		xrdObj := &unstructured.Unstructured{}
		xrdObj.SetAPIVersion("skycluster.io/v1alpha1")
		xrdObj.SetKind("XKube")
		xrdObj.SetNamespace(ns)
		xrdObj.SetName(appId)

		spec := map[string]any{
			"applicationId": appId,
			"serviceCidr":  k8sMetadata[pp.Spec.Platform][idx].ServiceCidr,
			"podCidr": func () map[string]any {
				fields := make(map[string]any)
				if pp.Spec.Platform == "aws" {
					fields["public"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Public
					fields["private"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Private
				}
				fields["cidr"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Cidr
				return fields
			}(),
			"nodeGroups": func () []map[string]any {
				fields := make([]map[string]any, 0)
				if pp.Spec.Platform == "aws" {
					// default node group
					f := make(map[string]any)
					f["instanceType"] = "4vCPU-8GB"
					f["publicAccess"] = true
					f["autoScaling"] = map[string]any{
						"enabled": false,
						"minSize": 1,
						"maxSize": 2,
					}
					fields = append(fields, f)

					// workers
					for _, vs := range computeProfileList {
						f := make(map[string]any)
						f["instanceType"] = vs.Name
						f["publicAccess"] = false
						fields = append(fields, f)
					}
				}
				if pp.Spec.Platform == "gcp" {
					// workers only
					for _, vs := range computeProfileList {
						f := make(map[string]any)
						f["nodeCount"] = 2
						f["instanceType"] = vs.Name
						f["publicAccess"] = false
						f["autoScaling"] = map[string]any{
							"enabled": true,
							"minSize": 1,
							"maxSize": 3,
						}
						fields = append(fields, f)
					}
				}
				return fields
			}(),
			"principal": func () map[string]any {
				fields := make(map[string]any)
				if pp.Spec.Platform == "aws" {
					fields["type"] = "servicePrincipal"
					fields["id"] = "arn:aws:iam::885707601199:root"
				}
				return fields
			}(),
			"providerRef": map[string]any{
				"name":   pName,
				"platform": pp.Spec.Platform,
				"region": pp.Spec.Region,
				"zones":  map[string]string{
					"primary": znPrimary.Name,
					"secondary": znSecondary.Name,
				},
			},
		}

		xrdObj.Object["spec"] = spec

		yamlObj, err := generateYAMLManifest(xrdObj)
		if err != nil {
			return nil, errors.Wrap(err, "Error generating YAML manifest.")
		}
		manifests[pName] = hv1a1.SkyService{
			ComponentRef: corev1.ObjectReference{
				APIVersion: xrdObj.GetAPIVersion(),
				Kind:       xrdObj.GetKind(),
				Namespace:  xrdObj.GetNamespace(),
				Name:       xrdObj.GetName(),
			},
			Manifest: yamlObj,
			ProviderRef: hv1a1.ProviderRefSpec{
				Name:   pName,
				Type: znPrimary.Type,
				Platform: pp.Spec.Platform,
				Region: pp.Spec.Region,
				RegionAlias: pp.Spec.RegionAlias,
				Zone:   znPrimary.Name,
			},
		}
		provLastIdxUsed[pName] = idx + 1
	}
	
	return manifests, nil
}

// dpRef is the deployment plan reference from SkyXRD spec
func (r *SkyXRDReconciler) fetchDeploymentPolicy(ns string, dpRef cv1a1.DeploymentPolicyRef) (*pv1a1.DeploymentPolicy, error) {
	dp := &pv1a1.DeploymentPolicy{}
	err := r.Get(context.TODO(), client.ObjectKey{Namespace: ns, Name: dpRef.Name}, dp)
	if err != nil {return nil, err}
	return dp, nil
}

// dpPolicy := r.fetchDeploymentPolicy(ns, dpRef)
// findReqVirtualSvcs(ns, skySrv, dpPolicy)
func (r *SkyXRDReconciler) findCheapestVirtualSvcs(ns string, providerRef hv1a1.ProviderRefSpec, virtualServices []pv1a1.VirtualServiceSelector) (*pv1a1.VirtualServiceSelector, error) {
	if len(virtualServices) == 0 {
		return nil, nil
	}

	virtSvcMap, err := r.fetchVirtualServiceCfgMap(ns, providerRef)
	if err != nil { return nil, errors.Wrap(err, "failed fetching virtual service config map") }

	// Find the set with the minimum cost
	var cheapestVirtSvc *pv1a1.VirtualServiceSelector
	minCost := math.MaxFloat64
	for _, vs := range virtualServices {
		
		cost, err := r.calculateVirtualServiceSetCost(virtSvcMap, vs, providerRef.Zone)
		if err != nil {return nil, err}

		if cost < minCost {
			minCost = cost
			cheapestVirtSvc = &vs
		}	
	}

	return cheapestVirtSvc, nil
}

func (r *SkyXRDReconciler) fetchVirtualServiceCfgMap(ns string, providerRef hv1a1.ProviderRefSpec) (map[string]string, error) {
	cm := &corev1.ConfigMapList{}
	if err := r.List(context.Background(), cm, client.MatchingLabels{
		"skycluster.io/config-type": "provider-profile",
		"skycluster.io/managed-by": "skycluster",
		"skycluster.io/provider-platform": providerRef.Platform,
		"skycluster.io/provider-profile": providerRef.Name,
		"skycluster.io/provider-region": providerRef.Region,
	}); err != nil { return nil, errors.Wrap(err, "listing provider profile ConfigMaps") }
	if len(cm.Items) == 0 { return nil, errors.New("no provider profile ConfigMaps found") }
	if len(cm.Items) > 1 { return nil, errors.New("multiple provider profile ConfigMaps found") }
	
	return cm.Items[0].Data, nil
}

// virtSvcMap := fetchVirtualServiceCfgMap(ns, providerRef)
// calculateVirtualServiceSetCost(virtSvcMap, virtualService, zone)
func (r *SkyXRDReconciler) calculateVirtualServiceSetCost(virtualSrvcMap map[string]string, vs pv1a1.VirtualServiceSelector, zone string) (float64, error) {
	
	// limited number of virtual services are supported: ManagedKubernetes, ComputeProfile
	switch vs.Kind {
	case "ManagedKubernetes":
		// cmData["managed-k8s.yaml"]
		mngK8sList := []hv1a1.ManagedK8s{}
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["managed-k8s.yaml"]), &mngK8sList); err != nil {
			return -1, errors.Wrap(err, "unmarshalling managed-k8s.yaml")
		}
		for _, mngK8s := range mngK8sList {
			if mngK8s.Name == vs.Name {
				// found matching managed K8s, calculate cost
				overheadCost, err := strconv.ParseFloat(mngK8s.Overhead.Cost, 64)
				if err != nil {return -1, errors.Wrap(err, "parsing overhead cost")}

				price, err := strconv.ParseFloat(mngK8s.Price, 64)
				if err != nil {return -1, errors.Wrap(err, "parsing price")}

				totalCost := price + (overheadCost * float64(vs.Count))
				return totalCost, nil
			}
		}

	case "ComputeProfile":
		// cmData["flavors.yaml"]
		var zoneOfferings []cv1a1.ZoneOfferings
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["flavors.yaml"]), &zoneOfferings); err == nil {
			
			for _, zo := range zoneOfferings {
				if zo.Zone != zone {continue}
				
				for _, of := range zo.Offerings {
					if of.Name != vs.Name {continue}
					
					priceFloat, err := strconv.ParseFloat(of.Price, 64)
					if err != nil {continue}
					
					return priceFloat, nil
				}
			}
		}
	}
	
	return -1, errors.New("unable to calculate cost for virtual service set")
}

// dpRef is the deployment plan reference from SkyXRD spec
// skySrvc is the SkyService component for which we are finding required virtual services
func (r *SkyXRDReconciler) findReqVirtualSvcs(ns string, skySrvc hv1a1.SkyService, dpPolicy pv1a1.DeploymentPolicy) ([]pv1a1.VirtualServiceSelector, error) {
	// parse the deployment plan to find required virtual services for the component
	cmpntRef := skySrvc.ComponentRef
	provRef := skySrvc.ProviderRef
	for _, cmpnt := range dpPolicy.Spec.DeploymentPolicies {

		if cmpnt.ComponentRef.Name == cmpntRef.Name && 
				cmpnt.ComponentRef.Kind == cmpntRef.Kind && 
				cmpnt.ComponentRef.APIVersion == cmpntRef.APIVersion {
		
			// found matching component, extract virtual services
			// list of (alternative virtual services)
			reqVirtualSvcs := make([]pv1a1.VirtualServiceSelector, 0) 
			for _, vsSet := range cmpnt.VirtualServiceConstraint {
				cheapestVirtualSvcs, err := r.findCheapestVirtualSvcs(ns, provRef, vsSet.AnyOf)
				if err != nil {return nil, errors.Wrap(err, "finding cheapest virtual services")}
				reqVirtualSvcs = append(reqVirtualSvcs, *cheapestVirtualSvcs)
			}
			// one component is matched, no need to continue
			return reqVirtualSvcs, nil
		}
	}
	return nil, errors.New("no matching component found in deployment policy")
}

// Load static metadata about k8s managed services from configmaps
// key: platform name, value: list of ManagedK8s metadata
func (r *SkyXRDReconciler) loadK8sSrvcMetadata() (map[string][]k8sMetadata, error) {
	k8sSrvcData, err := r.fetchK8sSrvcMetadataMap()
	if err != nil {return nil, errors.Wrap(err, "fetching k8s managed service metadata map")}
	
	k8sSrvcMetadataByKey := make(map[string][]k8sMetadata)
	for key, yamlStr := range k8sSrvcData {
		var metas []k8sMetadata
		if err := yaml.Unmarshal([]byte(yamlStr), &metas); err != nil {
			return nil, errors.Wrapf(err, "unmarshalling k8s service metadata for key %s", key)
		}
		k8sSrvcMetadataByKey[key] = metas
	}
	return k8sSrvcMetadataByKey, nil
}

// Load static metadata about k8s managed services from configmaps and return as a map
// key: platform name, value: yaml string 
func (r *SkyXRDReconciler) fetchK8sSrvcMetadataMap() (map[string]string, error) {
	cmList := &corev1.ConfigMapList{}
	if err := r.List(context.Background(), cmList, client.MatchingLabels{
		"skycluster.io/config-type": "k8s-cluster-metadata-e2e",
	}); err != nil {
		return nil, errors.Wrap(err, "listing k8s service metadata ConfigMaps")
	}

	if len(cmList.Items) == 0 {
		return nil, errors.New("no k8s service metadata ConfigMaps found")
	}
	if len(cmList.Items) > 1 {
		return nil, errors.New("more than one k8s service metadata ConfigMap found")
	}

	k8sSrvcData := make(map[string]string, len(cmList.Items[0].Data))
	for k, v := range cmList.Items[0].Data {
		k8sSrvcData[k] = v
	}
	return k8sSrvcData, nil
}

// Load static metadata about providers from configmaps
// key: platform name, value: list of provider metadata
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

// Load static metadata about providers from configmaps and return as a map
// key: platform name, value: yaml string
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

// Fetch all provider profiles and return as a map
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

func findSecondaryZone(p cv1a1.ProviderProfile, primary string) string {
	for _, z := range p.Spec.Zones {
		if z.Name != primary { return z.Name }
	}
	return ""
}


// 		// extra labels only for "SAVI" (openstack)
// 		if p.Platform == "openstack" {
// 			objAnnt := make(map[string]string, 0)
// 			objAnnt["skycluster.io/external-resources"] = "[{\"apiVersion\":\"identity.openstack.crossplane.io/v1alpha1\",\"kind\":\"ProjectV3\",\"id\":\"1e1c724728544ec2a9058303ddc8f30b\"}]"
// 			obj.SetAnnotations(objAnnt)
// 		}

	
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
// 			Name:   ctrlProvider.ProviderName,
// 			Region: ctrlProvider.ProviderRegion,
// 			Zone:   ctrlProvider.ProviderZone,
// 		},
// 	}, nil
// }

// func (r *SkyXRDReconciler) generateManagedK8SCluster(components []hv1a1.SkyService) (*hv1a1.SkyService, error) {
// 	depPerSvc := make(map[string][]hv1a1.SkyService, 0)
// 	for _, dp := range components {
// 		pName := dp.ProviderRef.Name
// 		depPerSvc[pName] = append(depPerSvc[pName], dp)
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
// 	for pName, deployments := range depPerSvc {
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
// 		for _, skyCmpnt := range depPerSvc[pName] {
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
// 		agentProvider := depPerSvc[pName][0].ProviderRef
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

