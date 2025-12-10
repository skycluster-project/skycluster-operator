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
	"math"
	"reflect"
	"slices"
	"sort"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"

	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	helper "github.com/skycluster-project/skycluster-operator/internal/helper"
)

// AtlasReconciler reconciles a Atlas object
type AtlasReconciler struct {
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
	ServiceCidr string `yaml:"serviceCidr,omitempty"`
	NodeCidr    string `yaml:"nodeCidr,omitempty"`
	PodCidr     PodCidrSpec `yaml:"podCidr,omitempty"`
}

type PodCidrSpec struct {
	Cidr 	string `yaml:"cidr"`
	Public string `yaml:"public,omitempty"`
	Private string `yaml:"private,omitempty"`
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=atlas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=atlas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=atlas/finalizers,verbs=update

func (r *AtlasReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started")

	// Fetch the object
	atlas := &cv1a1.Atlas{}
	if err := r.Get(ctx, req.NamespacedName, atlas); err != nil {
		r.Logger.Info("Atlas not found.")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	manifests, _,  err := r.createManifests(req.Name, req.Namespace, atlas)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error creating manifests.")
	}
	
	if atlas.Spec.Approve {
		for _, xrd := range manifests {
			var obj map[string]any
			if err := yaml.Unmarshal([]byte(xrd.Manifest), &obj); err != nil {
				r.Logger.Error(err, "error unmarshalling manifest yaml.", "manifest", xrd.Manifest)
				return ctrl.Result{}, err
			}

			svcKind := xrd.ComponentRef.Kind
			if svcKind != "XProvider" && svcKind != "XKube" {continue} // only create providers for now

			unstrObj := &unstructured.Unstructured{Object: obj}
			unstrObj.SetAPIVersion(xrd.ComponentRef.APIVersion)
			unstrObj.SetKind(xrd.ComponentRef.Kind)
			unstrObj.SetGenerateName(fmt.Sprintf("%s-", xrd.ComponentRef.Name))
			// if err := ctrl.SetControllerReference(atlas, unstrObj, r.Scheme); err != nil {
			// 	return ctrl.Result{}, errors.Wrap(err, "error setting controller reference.")
			// }
			if err := r.Create(ctx, unstrObj); err != nil && !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, client.IgnoreAlreadyExists(err)
			}
			// r.Logger.Info("Created Atlas object!", "name", xrd.ComponentRef.Name)
		}
	}

	// Sort manifests:
	sort.SliceStable(manifests, func(i, j int) bool {
		if manifests[i].ComponentRef.Kind != manifests[j].ComponentRef.Kind {
			return manifests[i].ComponentRef.Kind < manifests[j].ComponentRef.Kind
		}
		return manifests[i].ComponentRef.Name < manifests[j].ComponentRef.Name
	})

	changed := !reflect.DeepEqual(atlas.Status.Manifests, manifests)
	r.Logger.Info("Comparing manifests.", "oldCount", len(atlas.Status.Manifests), "newCount", len(manifests), "changed", changed)

	if changed {

		// Optionally log content at verbose level
		// var modified []int
		// for i := 0; i < min; i++ {
		// 	if !reflect.DeepEqual(old[i], manifests[i]) {
		// 		modified = append(modified, i)
		// 	}
		// }
		// r.Logger.Info("Manifests changed", "modifiedIndices", modified)
		// for _, i := range modified {
		// 	r.Logger.V(1).Info("Modified manifest", "index", i, "old", fmt.Sprintf("%+v", old[i]), "new", fmt.Sprintf("%+v", manifests[i]))
		// }

		atlas.Status.Manifests = manifests
		if err := r.Status().Update(ctx, atlas); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "error updating Atlas status")
		}
  }
	
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AtlasReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.Atlas{}).
		Named("core-atlas").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}

func (r *AtlasReconciler) createManifests(appId string, ns string, atlas *cv1a1.Atlas) ([]hv1a1.SkyService, []hv1a1.SkyService, error) {
	deployMap := atlas.Spec.DeployMap
	manifests := make([]hv1a1.SkyService, 0)
	
	// ######### Providers
	// Each deployment comes with component info (e.g. kind and apiVersion and name)
	// as well as the provider info (e.g. name, region, zone, type) that it should be deployed on
	// We extract all provider's info and create corresponding SkyProvider objects for each provider
	providersManifests, provToMetadataIdx, err := r.generateProviderManifests(appId, ns, deployMap.Component)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error generating provider manifests.")
	}
	if len(providersManifests) > 0 {
		manifests = append(manifests, lo.Values(providersManifests)...)
		r.Logger.Info("Generated provider manifests.", "count", len(providersManifests))
	}

	dpPolicy, err := r.fetchDeploymentPolicy(ns, atlas.Spec.DeploymentPolicyRef)
	if err != nil { return nil, nil, errors.Wrap(err, "fetching deployment policy") }

	// // ######### Handle execution environment: Kubernetes
	// if atlas.Spec.ExecutionEnvironment == "Kubernetes" {
	// 	k8sManifests, err := r.generateK8SManifests(appId, provToMetadataIdx, deployMap, *dpPolicy)
	// 	if err != nil {
	// 		return nil, nil, errors.Wrap(err, "Error generating Kubernetes manifests.")
	// 	}
	// 	if len(k8sManifests) > 0 {
	// 		manifests = append(manifests, k8sManifests...)
	// 	}
	// }

	// ######### Handle execution environment: VirtualMachine
	if atlas.Spec.ExecutionEnvironment == "VirtualMachine" {
		r.Logger.Info("Generating VirtualMachine manifests.")
		vmManifests, err := r.generateVMManifests(appId, provToMetadataIdx, deployMap, *dpPolicy)
		if err != nil {
			return nil, nil, errors.Wrap(err, "Error generating VirtualMachine manifests.")
		}
		if len(vmManifests) > 0 {
			manifests = append(manifests, vmManifests...)
			r.Logger.Info("Generated VirtualMachine manifests.", "count", len(vmManifests))
		}
	}


	// // virtual services required per provider
	// // currently only ComputeProfile kind is supported
	// requiredVirtSvcs := make(map[string][]pv1a1.VirtualServiceSelector)
	// for _, skySrvc := range deployMap.Component {
	// 	pName := skySrvc.ProviderRef.Name
	// 	pKind := skySrvc.ProviderRef.Type

	// 	supportedKinds := []string{"XInstance", "Deployment", "XNodeGroup"}
	// 	if !slices.Contains(supportedKinds, pKind) {
	// 		return nil, nil, errors.New("unsupported provider type for virtual service fetching: " + pKind)
	// 	}

	// 	if _, ok := requiredVirtSvcs[pName]; !ok {
	// 		requiredVirtSvcs[pName] = make([]pv1a1.VirtualServiceSelector, 0)
	// 	}
		
	// 	// find required cheapest virtual service for this component per each alternative set
	// 	// (requested ComputeProfile)
	// 	computeProfiles, err := r.findReqComputeProfile(ns, skySrvc, *dpPolicy)
	// 	if err != nil {return nil, nil, errors.Wrap(err, "fetching required virtual services")}
	// 	requiredVirtSvcs[pName] = append(requiredVirtSvcs[pName], computeProfiles...)
	// }

	// r.Logger.Info("Required virtual services per provider.", "providers", requiredVirtSvcs)

	// // If execution environment is Kubernetes, we need to create Kubernetes manifests
	// // We also need to include a set of ComputeProfiles as node groups for the cluster
	// // computeProfilesSvcs := make(map[string][]pv1a1.VirtualServiceSelector)
	// switch dpPolicy.Spec.ExecutionEnvironment {
	// case "Kubernetes" :
	// 	// when the execution environment is Kubernetes, we assume that all involved providers
	// 	// will be used to create a (managed) Kubernetes cluster. Therefore, a combination of 
	// 	// providers with Kubernetes clusters and without Kubernetes clusters is not supported.
	// 	// The reason is that we aim for running application, an we either support running it
	// 	// on a Kubernetes cluster (managed by us), or within VMs, not both at the same time.
	// 	// User is able to manually provision VMs along with managed Kubernetes if needed.
	// 	// We filter all ComputeProfile (flavors) and use them to create worker pools in the cluster
		
	// 	// for each provider, generate Kubernetes manifests, 
	// 	//   with node groups consolidated from all required virtual services for that provider
	// 	kubernetesManifests, err := r.generateMgmdK8sManifests(appId, requiredVirtSvcs, provToMetadataIdx)
	// 	if err != nil {
	// 		return nil, nil, errors.Wrap(err, "Error generating managed Kubernetes manifests.")
	// 	}
	// 	manifests = append(manifests, lo.Values(kubernetesManifests)...)

	// case "VirtualMachine" :
	// 	// for VirtualMachine execution environment, we need to create XInstance virtual services
	// 	// for each provider. In this case, it is expected to have component Kind of XInstance
	// 	// and the virtual services specify the ComputeProfiles.
	// 	// for _, skySrvc := range deployMap.Component {
	// 		// pName := skySrvc.ProviderRef.Name
	// 		// pKind := skySrvc.ProviderRef.Type

	// 		// if pKind != "XInstance" {
	// 		// 	r.Logger.Info("Skipping non-XInstance component for VirtualMachine execution environment.", "component", skySrvc.ComponentRef.Name, "kind", pKind)
	// 		// 	continue
	// 		// }
	// 	// }
	// 		// requiredVirtSvcs[pName] = lo.UniqBy(requiredVirtSvcs[pName], func(v pv1a1.VirtualServiceSelector) string {
		
	// default:
	// 	return nil, nil, errors.New("unsupported execution environment: " + dpPolicy.Spec.ExecutionEnvironment)
	// }
	
	// Handle ManagedKubernetes virtual services
	// managedK8sSvcs contains ComputeProfile (flavors) for the cluster
	// r.Logger.Info("Generating SkyK8SCluster manifests.")
	// if len(managedK8sSvcs) > 0 {
	// 	managedK8sManifests, err := r.generateMgmdK8sManifests(appId, managedK8sSvcs, provToMetadataIdx)
	// 	if err != nil {
	// 		return nil, nil, errors.Wrap(err, "Error generating SkyK8SCluster.")
	// 	}
	// 	for _, obj := range managedK8sManifests {
	// 		manifests = append(manifests, obj)
	// 	}
	// 	r.Logger.Info("Generated SkyK8SCluster manifests.", "count", len(managedK8sManifests))
	// 	// Kubernetes Mesh
	// 	// r.Logger.Info("Generating XKubeMesh manifests.")
	// 	skyMesh, err := r.generateK8sMeshManifests(appId, lo.Values(managedK8sManifests))
	// 	if err != nil {
	// 		return nil, nil, errors.Wrap(err, "Error generating XKubeMesh.")
	// 	}
	// 	manifests = append(manifests, *skyMesh)
	// }


	return manifests, nil, nil
}

// Creates manifest for setup of providers needed for the deployment
func (r *AtlasReconciler) generateProviderManifests(appId string, ns string, cmpnts []hv1a1.SkyService) (map[string]hv1a1.SkyService, map[string]map[string]int, error) {
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
	// a map from platform to map[providerName]index in metadata list
	// aws -> { us-east-1a: 0, us-east-1b: 1 }
	indexedSortedProviders := buildIndexMap(uniqueProviders)
	
	// Now we have all unique providers
	// We can now generate the manifests for each provider
	// we use settings corresponding to each provider stored in a configmap (ip ranges, etc.)
	provMetadata, err := r.loadProviderMetadata()
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error loading provider metadata.")
	}

	providerProfiles, err := r.fetchProviderProfilesMap()
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error fetching provider profiles.")
	}

	manifests := map[string]hv1a1.SkyService{}
	for pName, p := range uniqueProviders {
		if p.Platform == "baremetal" { continue } // skip baremetal provider creation

		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("skycluster.io/v1alpha1")
		obj.SetKind("XProvider")

		// set name with deterministic random suffix to avoid name collisions
		name := helper.EnsureK8sName("xp-" + pName + "-" + appId)
		rand := RandSuffix(name)
		name = name[0:int(math.Min(float64(len(name)), 15))]
		name = name + "-" + rand
		obj.SetName(name)

		znPrimary, ok := lo.Find(providerProfiles[pName].Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
		if !ok {return nil, nil, errors.New("No primary zone found")}
		znSecondary, ok := lo.Find(providerProfiles[pName].Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.Name != znPrimary.Name })
		if !ok && !slices.Contains([]string{"baremetal", "openstack"}, p.Platform) {return nil, nil, errors.New("No secondary zone found")}
		
		// prepare static values and subnets
		idx := indexedSortedProviders[p.Platform][pName]
		cidr := provMetadata[p.Platform][idx].VPCCIDR
		zoneCount := len(lo.Filter(providerProfiles[pName].Spec.Zones, func(z cv1a1.ZoneSpec, _ int) bool { 
			return z.Enabled
		}))
		subnets, err := helper.Subnets(cidr, zoneCount)
		if err != nil { return nil, nil, errors.Wrap(err, "Error calculating subnets.") }

		spec := map[string]any{
			"applicationId": appId,
			"vpcCidr":  func() string {
				if p.Platform == "aws" || p.Platform == "gcp" || p.Platform == "openstack" {
					return provMetadata[p.Platform][idx].VPCCIDR
				}
				// not supported. Baremetal xprovider expected to be created manually.
				return ""
			}(),
			"gateway": func() map[string]any {
				fields := make(map[string]any)
				if p.Platform == "aws" {
					fields["volumeType"] = "gp2"
					fields["volumeSize"] = 30
				}
				fields["flavor"] = func () string {
					if p.Platform == "aws" || p.Platform == "openstack" {return "4vCPU-16GB"}
					return "2vCPU-8GB"
				}()
				return fields
			}(),
			"subnets": func() []map[string]any {
				subnetList := make([]map[string]any, 0)
				for i := range zoneCount {
					sn := subnets[i]
					subnet := map[string]any{
						"cidr": sn.String(),
						"zone": providerProfiles[pName].Spec.Zones[i].Name,
					}
					if p.Platform == "aws" {
						subnet["type"] = providerProfiles[pName].Spec.Zones[i].Type
					}
					if p.Platform == "baremetal" && providerProfiles[pName].Spec.Zones[i].Type == "public" {
						subnet["default"] = true
					} 
					if p.Platform == "openstack" {
						subnet["default"] = true
					} 
					subnetList = append(subnetList, subnet)
				}
				return subnetList
			}(),
			"externalNetwork": func() map[string]string {
				if p.Platform != "openstack" {
					return nil
				}
				switch p.Region {
				case "SCINET": // SAVI Testbed
					return map[string]string{
						"networkName": "ext-net",
						"networkId":   "0a23c4ae-98ba-4f3a-8c85-5a7dc614cc4e",
						"subnetName":  "ext-subnet",
						"subnetId":    "ae9a8eac-5f6f-4681-98f6-71acf18dcfbb",
					}
				case "VAUGHAN":
					return map[string]string{
						"networkName": "ext-net",
						"networkId":   "ac6641d9-9a20-4796-b95c-24254834c7e8",
						"subnetName":  "ext-subnet",
						"subnetId":    "6da2a42c-d746-4a95-aae0-e5807ce0250d",
					}
				}
				return nil
			}(),
			"providerRef": map[string]any{
				"platform": p.Platform,
				"region": p.Region,
				"zones":  map[string]string{
					"primary": znPrimary.Name,
					"secondary": znSecondary.Name,
				},
			},
		}
		obj.Object["spec"] = spec

		// extra labels only for "SAVI Testbed" (openstack)
		if p.Platform == "openstack" {
			objAnnt := make(map[string]string, 0)
			objAnnt["skycluster.io/external-resources"] = "[{\"apiVersion\":\"identity.openstack.crossplane.io/v1alpha1\",\"kind\":\"ProjectV3\",\"id\":\"1e1c724728544ec2a9058303ddc8f30b\"}]"
			obj.SetAnnotations(objAnnt)
		}
		
		yamlObj, err := generateYAMLManifest(obj)
		if err != nil {
			return nil, nil, errors.Wrap(err, "Error generating YAML manifest.")
		}
		manifests[pName] = hv1a1.SkyService{
			ComponentRef: hv1a1.ComponentRef{
				APIVersion: obj.GetAPIVersion(),
				Kind:       obj.GetKind(),
				// Namespace:  obj.GetNamespace(),
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
	return manifests, indexedSortedProviders, nil
}

// Generates VM manifests based on deployment map and deployment policy
// It fetches uses deployment policy object to determine requested instance types
// and find equivalent virtual services (ComputeProfile) for the specified provider
func (r *AtlasReconciler) generateVMManifests(appId string, provToIdx map[string]map[string]int, deployMap cv1a1.DeployMap, dpPolicy pv1a1.DeploymentPolicy) ([]hv1a1.SkyService, error) {

	manifests := make([]hv1a1.SkyService, 0)
	for _, cmpnt := range deployMap.Component {
		if cmpnt.ComponentRef.Kind != "XInstance" {continue}

		// find requested ComputeProfile virtual service for this component
		computeProfileList, err := r.findReqComputeProfile(dpPolicy.Namespace, cmpnt, dpPolicy)
		if err != nil {
			return nil, errors.Wrap(err, "Error finding requested ComputeProfile.")
		}
		if len(computeProfileList) == 0 {
			return nil, errors.New("No ComputeProfile found for component: " + cmpnt.ComponentRef.Name)
		}
		// we take the first one as the best fit
		// computeProfile := computeProfileList[0]

		platform := cmpnt.ProviderRef.Platform
		region := cmpnt.ProviderRef.Region
		zone := cmpnt.ProviderRef.Zone

		// we need to create a XKube object
		xrdObj := &unstructured.Unstructured{}
		xrdObj.SetAPIVersion("skycluster.io/v1alpha1")
		xrdObj.SetKind("XInstance")

		name := helper.EnsureK8sName("xvm-" + "-" + appId)
		rand := RandSuffix(name)
		name = name[0:int(math.Min(float64(len(name)), 15))]
		name = name + "-" + rand
		xrdObj.SetName(name)

		spec := map[string]any{
			"applicationId": appId,
			"providerRef": func () map[string]any {
				fields := map[string]any{
					"platform": platform,
					"region": region,
					"zone": zone,
				}
				return fields
			}(),
		}

		xrdObj.Object["spec"] = spec

		yamlObj, err := generateYAMLManifest(xrdObj)
		if err != nil {
			return nil, errors.Wrap(err, "Error generating YAML manifest.")
		}
		manifests = append(manifests, hv1a1.SkyService{
			ComponentRef: hv1a1.ComponentRef{
				APIVersion: xrdObj.GetAPIVersion(),
				Kind:       xrdObj.GetKind(),
				// Namespace:  xrdObj.GetNamespace(),
				Name:       xrdObj.GetName(),
			},
			Manifest: yamlObj,
			ProviderRef: hv1a1.ProviderRefSpec{
				// Name:   pName,
				// Type: znPrimary.Type,
				Platform: platform,
				Region: region,
				// RegionAlias: pp.Spec.RegionAlias,
				Zone:   zone,
			},
		})
	}
	return manifests, nil
}

// 	// For managed K8S deployments, we start the cluster with nodes needed for control plane
// 	// and let the cluster autoscaler handle the rest of the scaling for the workload.
// 	// The optimization has decided that for requested workload, this provider is the best fit.

// 	k8sMetadata, err := r.loadProviderMetadata()
// 	if err != nil {return nil, errors.Wrap(err, "Error loading provider metadata.")}

// 	provProfiles, err := r.fetchProviderProfilesMap()
// 	if err != nil {return nil, errors.Wrap(err, "Error fetching provider profiles.")}

// 	manifests := map[string]hv1a1.SkyService{}
// 	for pName, skySvc := range deployMap.Component {
// 		// computeProfileList is the list of ComputeProfile virtual services for this provider
// 		// r.Logger.Info("Compute profiles for provider", "profiles", len(computeProfileList), "provider", pName)
// 		uniqueProfiles := lo.UniqBy(computeProfileList, func(v pv1a1.VirtualServiceSelector) string {
// 			return v.Name
// 		})
// 		// r.Logger.Info("Unique compute profiles for provider", "profiles", len(uniqueProfiles), "provider", pName)
// 		pp := provProfiles[pName]
// 		idx := provToIdx[pp.Spec.Platform][pName]

// 		znPrimary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
// 		if !ok {return nil, errors.New("No primary zone found")}
// 		znSecondary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.Name != znPrimary.Name })
// 		if !ok && !slices.Contains([]string{"baremetal", "openstack"}, pp.Spec.Platform) {return nil, errors.New("No secondary zone found")}

// 		// we need to create a XKube object
// 		xrdObj := &unstructured.Unstructured{}
// 		xrdObj.SetAPIVersion("skycluster.io/v1alpha1")
// 		xrdObj.SetKind("XKube")
// 		name := helper.EnsureK8sName("xk-" + pName + "-" + appId)
// 		rand := RandSuffix(name)
// 		name = name[0:int(math.Min(float64(len(name)), 15))]
// 		name = name + "-" + rand
// 		xrdObj.SetName(name)

// 		spec := map[string]any{
// 			"applicationId": appId,
// 			"serviceCidr":  k8sMetadata[pp.Spec.Platform][idx].ServiceCidr,
// 			"nodeCidr": func () string {
// 				if pp.Spec.Platform == "gcp" {
// 					return k8sMetadata[pp.Spec.Platform][idx].NodeCidr
// 				}
// 				return ""
// 			}(),
// 			"podCidr": func () map[string]any {
// 				fields := make(map[string]any)
// 				if pp.Spec.Platform == "aws" {
// 					fields["public"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Public
// 					fields["private"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Private
// 				}
// 				fields["cidr"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Cidr
// 				return fields
// 			}(),
// 			"nodeGroups": func () []map[string]any {
// 				fields := make([]map[string]any, 0)
// 				if pp.Spec.Platform == "aws" {
// 					// default node group
// 					f := make(map[string]any)
// 					f["instanceTypes"] = []string{
// 						"2vCPU-4GB",
// 						"4vCPU-8GB",
// 						"8vCPU-32GB",
// 					}
// 					f["nodeCount"] = 3
// 					f["publicAccess"] = true
// 					f["autoScaling"] = map[string]any{
// 						"enabled": false,
// 						"minSize": 1,
// 						"maxSize": 3,
// 					}
// 					fields = append(fields, f)

// 					// workers
// 					for _, vs := range uniqueProfiles {
// 						f := make(map[string]any)
// 						f["instanceTypes"] = []string{vs.Name}
// 						f["publicAccess"] = false
// 						fields = append(fields, f)
// 					}
// 				}
// 				if pp.Spec.Platform == "gcp" {
// 					// workers only
// 					// manually limit this to prevent charging too much
// 					f := make(map[string]any)
// 						f["nodeCount"] = 3
// 						f["instanceType"] = "2vCPU-4GB"
// 						f["publicAccess"] = false
// 						f["autoScaling"] = map[string]any{
// 							"enabled": true,
// 							"minSize": 1,
// 							"maxSize": 8,
// 					}
// 					fields = append(fields, f)
// 					// for _, vs := range uniqueProfiles {
// 					// 	f := make(map[string]any)
// 					// 	f["nodeCount"] = 2
// 					// 	f["instanceType"] = vs.Name
// 					// 	f["publicAccess"] = false
// 					// 	f["autoScaling"] = map[string]any{
// 					// 		"enabled": true,
// 					// 		"minSize": 1,
// 					// 		"maxSize": 3,
// 					// 	}
// 					// 	fields = append(fields, f)
// 					// }
// 				}
// 				return fields
// 			}(),
// 			"principal": func () map[string]any {
// 				fields := make(map[string]any)
// 				if pp.Spec.Platform == "aws" {
// 					fields["type"] = "servicePrincipal"
// 					fields["id"] = "arn:aws:iam::885707601199:root"
// 				}
// 				return fields
// 			}(),
// 			"providerRef": func () map[string]any {
// 				fields := map[string]any{
// 					"platform": pp.Spec.Platform,
// 					"region": pp.Spec.Region,
// 				}
// 				if pp.Spec.Platform == "aws" {
// 					fields["zones"] = map[string]string{
// 						"primary": znPrimary.Name,
// 						"secondary": znSecondary.Name,
// 					}
// 				}
// 				if pp.Spec.Platform == "gcp" {
// 					fields["zones"] = map[string]string{
// 						"primary": znPrimary.Name,
// 					}
// 				}
// 				return fields
// 			}(),
// 		}

// 		xrdObj.Object["spec"] = spec

// 		yamlObj, err := generateYAMLManifest(xrdObj)
// 		if err != nil {
// 			return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 		}
// 		manifests[pName] = hv1a1.SkyService{
// 			ComponentRef: corev1.ObjectReference{
// 				APIVersion: xrdObj.GetAPIVersion(),
// 				Kind:       xrdObj.GetKind(),
// 				Namespace:  xrdObj.GetNamespace(),
// 				Name:       xrdObj.GetName(),
// 			},
// 			Manifest: yamlObj,
// 			ProviderRef: hv1a1.ProviderRefSpec{
// 				Name:   pName,
// 				Type: znPrimary.Type,
// 				Platform: pp.Spec.Platform,
// 				Region: pp.Spec.Region,
// 				RegionAlias: pp.Spec.RegionAlias,
// 				Zone:   znPrimary.Name,
// 			},
// 		}
// 	}
	
// 	return manifests, nil
// }

// func (r *AtlasReconciler) generateK8SManifests(appId string, provToIdx map[string]map[string]int, deployMap hv1a1.DeployMap, dpPolicy pv1a1.DeploymentPolicy) (map[string]hv1a1.SkyService, error) {
// 	// For managed K8S deployments, we start the cluster with nodes needed for control plane
// 	// and let the cluster autoscaler handle the rest of the scaling for the workload.
// 	// The optimization has decided that for requested workload, this provider is the best fit.

// 	k8sMetadata, err := r.loadProviderMetadata()
// 	if err != nil {return nil, errors.Wrap(err, "Error loading provider metadata.")}

// 	provProfiles, err := r.fetchProviderProfilesMap()
// 	if err != nil {return nil, errors.Wrap(err, "Error fetching provider profiles.")}

// 	manifests := map[string]hv1a1.SkyService{}
// 	for pName, skySvc := range deployMap.Component {
// 		// computeProfileList is the list of ComputeProfile virtual services for this provider
// 		// r.Logger.Info("Compute profiles for provider", "profiles", len(computeProfileList), "provider", pName)
// 		uniqueProfiles := lo.UniqBy(computeProfileList, func(v pv1a1.VirtualServiceSelector) string {
// 			return v.Name
// 		})
// 		// r.Logger.Info("Unique compute profiles for provider", "profiles", len(uniqueProfiles), "provider", pName)
// 		pp := provProfiles[pName]
// 		idx := provToIdx[pp.Spec.Platform][pName]

// 		znPrimary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
// 		if !ok {return nil, errors.New("No primary zone found")}
// 		znSecondary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.Name != znPrimary.Name })
// 		if !ok && !slices.Contains([]string{"baremetal", "openstack"}, pp.Spec.Platform) {return nil, errors.New("No secondary zone found")}

// 		// we need to create a XKube object
// 		xrdObj := &unstructured.Unstructured{}
// 		xrdObj.SetAPIVersion("skycluster.io/v1alpha1")
// 		xrdObj.SetKind("XKube")
// 		name := helper.EnsureK8sName("xk-" + pName + "-" + appId)
// 		rand := RandSuffix(name)
// 		name = name[0:int(math.Min(float64(len(name)), 15))]
// 		name = name + "-" + rand
// 		xrdObj.SetName(name)

// 		spec := map[string]any{
// 			"applicationId": appId,
// 			"serviceCidr":  k8sMetadata[pp.Spec.Platform][idx].ServiceCidr,
// 			"nodeCidr": func () string {
// 				if pp.Spec.Platform == "gcp" {
// 					return k8sMetadata[pp.Spec.Platform][idx].NodeCidr
// 				}
// 				return ""
// 			}(),
// 			"podCidr": func () map[string]any {
// 				fields := make(map[string]any)
// 				if pp.Spec.Platform == "aws" {
// 					fields["public"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Public
// 					fields["private"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Private
// 				}
// 				fields["cidr"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Cidr
// 				return fields
// 			}(),
// 			"nodeGroups": func () []map[string]any {
// 				fields := make([]map[string]any, 0)
// 				if pp.Spec.Platform == "aws" {
// 					// default node group
// 					f := make(map[string]any)
// 					f["instanceTypes"] = []string{
// 						"2vCPU-4GB",
// 						"4vCPU-8GB",
// 						"8vCPU-32GB",
// 					}
// 					f["nodeCount"] = 3
// 					f["publicAccess"] = true
// 					f["autoScaling"] = map[string]any{
// 						"enabled": false,
// 						"minSize": 1,
// 						"maxSize": 3,
// 					}
// 					fields = append(fields, f)

// 					// workers
// 					for _, vs := range uniqueProfiles {
// 						f := make(map[string]any)
// 						f["instanceTypes"] = []string{vs.Name}
// 						f["publicAccess"] = false
// 						fields = append(fields, f)
// 					}
// 				}
// 				if pp.Spec.Platform == "gcp" {
// 					// workers only
// 					// manually limit this to prevent charging too much
// 					f := make(map[string]any)
// 						f["nodeCount"] = 3
// 						f["instanceType"] = "2vCPU-4GB"
// 						f["publicAccess"] = false
// 						f["autoScaling"] = map[string]any{
// 							"enabled": true,
// 							"minSize": 1,
// 							"maxSize": 8,
// 					}
// 					fields = append(fields, f)
// 					// for _, vs := range uniqueProfiles {
// 					// 	f := make(map[string]any)
// 					// 	f["nodeCount"] = 2
// 					// 	f["instanceType"] = vs.Name
// 					// 	f["publicAccess"] = false
// 					// 	f["autoScaling"] = map[string]any{
// 					// 		"enabled": true,
// 					// 		"minSize": 1,
// 					// 		"maxSize": 3,
// 					// 	}
// 					// 	fields = append(fields, f)
// 					// }
// 				}
// 				return fields
// 			}(),
// 			"principal": func () map[string]any {
// 				fields := make(map[string]any)
// 				if pp.Spec.Platform == "aws" {
// 					fields["type"] = "servicePrincipal"
// 					fields["id"] = "arn:aws:iam::885707601199:root"
// 				}
// 				return fields
// 			}(),
// 			"providerRef": func () map[string]any {
// 				fields := map[string]any{
// 					"platform": pp.Spec.Platform,
// 					"region": pp.Spec.Region,
// 				}
// 				if pp.Spec.Platform == "aws" {
// 					fields["zones"] = map[string]string{
// 						"primary": znPrimary.Name,
// 						"secondary": znSecondary.Name,
// 					}
// 				}
// 				if pp.Spec.Platform == "gcp" {
// 					fields["zones"] = map[string]string{
// 						"primary": znPrimary.Name,
// 					}
// 				}
// 				return fields
// 			}(),
// 		}

// 		xrdObj.Object["spec"] = spec

// 		yamlObj, err := generateYAMLManifest(xrdObj)
// 		if err != nil {
// 			return nil, errors.Wrap(err, "Error generating YAML manifest.")
// 		}
// 		manifests[pName] = hv1a1.SkyService{
// 			ComponentRef: corev1.ObjectReference{
// 				APIVersion: xrdObj.GetAPIVersion(),
// 				Kind:       xrdObj.GetKind(),
// 				Namespace:  xrdObj.GetNamespace(),
// 				Name:       xrdObj.GetName(),
// 			},
// 			Manifest: yamlObj,
// 			ProviderRef: hv1a1.ProviderRefSpec{
// 				Name:   pName,
// 				Type: znPrimary.Type,
// 				Platform: pp.Spec.Platform,
// 				Region: pp.Spec.Region,
// 				RegionAlias: pp.Spec.RegionAlias,
// 				Zone:   znPrimary.Name,
// 			},
// 		}
// 	}
	
// 	return manifests, nil
// }

// only one mesh per application
func (r *AtlasReconciler) generateK8sMeshManifests(appId string, xKubeList []hv1a1.SkyService) (*hv1a1.SkyService, error) {
	
	clusterNames := make([]string, 0)
	for _, xkube := range xKubeList {
		kName := xkube.ComponentRef.Name
		clusterNames = append(clusterNames, kName)
	}

	// need to sort cluster names to ensure consistent naming
	sort.SliceStable(clusterNames, func(i, j int) bool {
		return clusterNames[i] < clusterNames[j]
	})
	
	// we need to create a XKube object
	xrdObj := &unstructured.Unstructured{}
	xrdObj.SetAPIVersion("skycluster.io/v1alpha1")
	xrdObj.SetKind("XKubeMesh")
	name := helper.EnsureK8sName("xkmesh-" + appId)
	rand := RandSuffix(name)
	name = name[0:int(math.Min(float64(len(name)), 15))]
	name = name + "-" + rand
	xrdObj.SetName(name)

	spec := map[string]any{
		"clusterNames": clusterNames,
		"localCluster": func () map[string]any {
			return map[string]any{
				"podCidr": "10.0.0.0/19",
				"serviceCidr": "10.0.32.0/19",
			}
		}(),
	}

	xrdObj.Object["spec"] = spec

	yamlObj, err := generateYAMLManifest(xrdObj)
	if err != nil {
		return nil, errors.Wrap(err, "Error generating YAML manifest.")
	}
	return &hv1a1.SkyService{
		ComponentRef: hv1a1.ComponentRef{
			APIVersion: xrdObj.GetAPIVersion(),
			Kind:       xrdObj.GetKind(),
			// Namespace:  xrdObj.GetNamespace(),
			Name:       xrdObj.GetName(),
		},
		Manifest: yamlObj,
	}, nil
	
}